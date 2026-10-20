package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jom-io/codex-link/internal/link"
	"github.com/jom-io/codex-link/internal/wake"
)

// The native host owns this stdio child. No executable or command supplied by an
// incoming message is ever run, and no remote listener or model key is needed.
func serveWake(home string, args []string) error {
	if len(args) == 0 || (args[0] != "watch" && args[0] != "status") {
		return errors.New("usage: wake watch --session ALIAS --native-session ID --from DEVICE_IDS [--conversation LABELS] [--state FILE]; wake status --state FILE")
	}
	fs := flag.NewFlagSet("wake "+args[0], flag.ContinueOnError)
	var c wake.Config
	var sources, conversations, kinds string
	fs.BoolVar(&c.Registry, "registry", false, "recheck persistent per-chat channel scopes before native wake")
	fs.StringVar(&c.Session, "session", "", "unique local worker session bound to this chat")
	fs.StringVar(&c.NativeSession, "native-session", "", "actual harness chat ID; never inferred from a worker alias")
	fs.StringVar(&sources, "from", "", "comma-separated allowed device IDs; self means this device")
	fs.StringVar(&conversations, "conversation", "", "optional comma-separated conversation allowlist")
	fs.StringVar(&kinds, "kinds", "task,result", "wake kinds; heartbeats and receipts are never accepted")
	fs.StringVar(&c.StatePath, "state", "", "absolute private cursor/queue file; default is in the existing data directory")
	fs.Int64Var(&c.After, "after", -1, "first start cursor; -1 starts after current tail; persisted state wins")
	fs.DurationVar(&c.Cooldown, "cooldown", 5*time.Second, "minimum interval between native acceptances, 1s–1m")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected wake arguments")
	}
	if args[0] == "status" {
		if !filepath.IsAbs(c.StatePath) {
			return errors.New("wake status requires an absolute --state FILE")
		}
		b, err := os.ReadFile(c.StatePath)
		if err != nil {
			return err
		}
		var state wake.State
		if json.Unmarshal(b, &state) != nil {
			return errors.New("invalid wake state")
		}
		lockHeld := false
		if lock, e := os.OpenFile(c.StatePath+".lock", os.O_RDWR, 0); e == nil {
			if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
				lockHeld = true
			} else {
				syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
			}
			lock.Close()
		}
		return printJSON(map[string]any{"state": state, "listener_lock_held": lockHeld})
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	call := func(ctx context.Context, op string, a link.APIRequest) (json.RawMessage, error) {
		bounded, stop := context.WithTimeout(ctx, 35*time.Second)
		defer stop()
		return link.Call(bounded, home, op, a)
	}
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(s, ",")
	}
	c.From = split(sources)
	c.Conversations = split(conversations)
	c.Kinds = split(kinds)
	for i, source := range c.From {
		if source == "self" {
			raw, err := call(ctx, "status", link.APIRequest{})
			if err != nil {
				return err
			}
			var status struct {
				ID string `json:"device_id"`
			}
			if json.Unmarshal(raw, &status) != nil {
				return errors.New("invalid daemon status")
			}
			c.From[i] = status.ID
		}
	}
	if c.StatePath == "" {
		h := sha256.Sum256([]byte(c.Session + "\x00" + c.NativeSession))
		c.StatePath = filepath.Join(home, "wake", hex.EncodeToString(h[:16])+".json")
	}
	w, err := wake.Open(ctx, c, call)
	if err != nil {
		return err
	}
	defer w.Close()
	encoder := json.NewEncoder(os.Stdout)
	if err = encoder.Encode(map[string]any{"type": "ready", "session": c.Session, "native_session": c.NativeSession, "state_path": c.StatePath, "cursor": w.Snapshot().Cursor}); err != nil {
		return err
	}
	acks := make(chan wake.Ack, 1)
	readErrors := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 4096), 64*1024)
		for scanner.Scan() {
			var ack wake.Ack
			if json.Unmarshal(scanner.Bytes(), &ack) != nil {
				readErrors <- errors.New("invalid wake acknowledgment")
				return
			}
			select {
			case acks <- ack:
			case <-ctx.Done():
				return
			}
		}
		readErrors <- errors.New("wake host disconnected")
	}()
	deliver := func(ctx context.Context, event wake.Event) (wake.Ack, error) {
		if err := encoder.Encode(event); err != nil {
			return wake.Ack{}, err
		}
		timer := time.NewTimer(15 * time.Second)
		defer timer.Stop()
		select {
		case ack := <-acks:
			return ack, nil
		case err := <-readErrors:
			cancel()
			return wake.Ack{}, err
		case <-timer.C:
			return wake.Ack{}, fmt.Errorf("wake host did not acknowledge %s", event.ID)
		case <-ctx.Done():
			return wake.Ack{}, ctx.Err()
		}
	}
	return w.Run(ctx, deliver)
}
