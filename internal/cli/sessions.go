package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jom-io/codex-link/internal/link"
)

func nativeContext(a *link.APIRequest) error {
	if a.NativeSession == "" {
		if id := os.Getenv("CODEX_THREAD_ID"); id != "" {
			a.NativeSession = id
			if a.Harness == "" {
				a.Harness = "codex"
			}
		} else if id := os.Getenv("DSH_SESSION_ID"); id != "" {
			a.NativeSession = id
			if a.Harness == "" {
				a.Harness = "deepseek-harness"
			}
		}
	}
	if a.NativeSession == "" {
		return errors.New("current chat context unavailable; supply host-provided --native-session and --harness, never select a recent chat")
	}
	if a.Harness == "" {
		return errors.New("host harness required")
	}
	if a.Profile == "" {
		a.Profile = "default"
	}
	return nil
}
func currentSession(ctx context.Context, home string, a *link.APIRequest) error {
	if a.Session != "" {
		return nil
	}
	if e := nativeContext(a); e != nil {
		return e
	}
	raw, e := link.Call(ctx, home, "session/current", *a)
	if e != nil {
		return e
	}
	var ep link.Endpoint
	if e = json.Unmarshal(raw, &ep); e != nil {
		return e
	}
	a.Session = ep.Session
	return nil
}
func legacyWorker(home string, a *link.APIRequest) {
	if a.Session != "" {
		return
	}
	paths, _ := filepath.Glob(filepath.Join(home, "wake", "*.json"))
	for _, p := range paths {
		b, e := os.ReadFile(p)
		if e != nil {
			continue
		}
		var old struct {
			Session string `json:"session"`
			Native  string `json:"nativeSessionId"`
			State   string `json:"statePath"`
			CLI     string `json:"cliPath"`
		}
		if json.Unmarshal(b, &old) == nil && old.Native == a.NativeSession && old.CLI != "" && old.State != "" && link.ValidLabel(old.Session) {
			a.Session = old.Session
			a.Path = old.State
			return
		}
	}
}
func serveSessions(home string, args []string) error {
	op := args[0]
	rest := args[1:]
	if op == "session" {
		if len(rest) == 0 {
			return errors.New("usage: session attach|list|current|pause|resume")
		}
		op += "/" + rest[0]
		rest = rest[1:]
	}
	switch op {
	case "session/verify", "session/attach", "session/list", "session/current", "session/pause", "session/resume", "session/receiver", "session/presence", "session/gc", "session/detach", "session/pin", "session/unpin", "bind", "bindings", "unbind":
	default:
		return errors.New("unknown session operation")
	}
	var a link.APIRequest
	if op == "session/list" {
		a.View = "active"
	}
	fs := flag.NewFlagSet(op, flag.ContinueOnError)
	var listAll, archived, dryRun bool
	fs.BoolVar(&listAll, "all", false, "include offline and archived registry records")
	fs.BoolVar(&archived, "archived", false, "show archived/withdrawn records")
	fs.BoolVar(&dryRun, "dry-run", false, "preview only (gc default)")
	fs.BoolVar(&a.Apply, "apply", false, "apply safe soft archival; no history deletion")
	fs.StringVar(&a.View, "view", a.View, "active, all or archived")
	fs.Int64Var(&a.After, "after", 0, "list offset")
	fs.IntVar(&a.Limit, "limit", 0, "maximum list records; zero means all matches")
	fs.StringVar(&a.Session, "session", "", "retained full worker; inferred from this native chat if omitted")
	fs.StringVar(&a.NativeSession, "native-session", "", "native ID supplied by this chat's host context")
	fs.StringVar(&a.Harness, "harness", "", "codex, deepseek-harness, claude-code, or another host")
	fs.StringVar(&a.Profile, "profile", "default", "host configuration instance")
	fs.StringVar(&a.Name, "name", "", "optional display alias; stable device/host/time/short-ID default")
	fs.StringVar(&a.Agent, "agent", link.Environment("AGENT"), "worker name prefix")
	fs.StringVar(&a.To, "to", "", "paired device to request a directory from")
	fs.StringVar(&a.Channel, "channel", "", "cooperation channel name or ID")
	fs.StringVar(&a.ID, "probe", "", "exact received binding probe task ID; claim first")
	fs.StringVar(&a.ID, "accept", "", "accept this exact invitation in the current chat")
	var messages string
	fs.StringVar(&messages, "messages", "", "native adapter admitted message IDs, comma separated")
	fs.StringVar(&a.Code, "event", "", "native adapter admission event ID")
	fs.StringVar(&a.State, "state", "", "local receiver state")
	fs.StringVar(&a.Path, "state-path", "", "existing private wake queue to retain on import")
	fs.BoolVar(&a.EnableWake, "wake", false, "enable this chat's native receiver")
	fs.BoolVar(&a.TextWake, "text-wake", false, "also wake for addressed text from accepted peers")
	fs.Bool("json", true, "JSON output")
	// flag stops at the first positional target; accept the documented bind TARGET FLAGS form.
	targets := []string{}
	if op == "bind" {
		for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			targets = append(targets, rest[0])
			rest = rest[1:]
		}
	}
	if e := fs.Parse(rest); e != nil {
		return e
	}
	if op == "bind" {
		targets = append(targets, fs.Args()...)
	} else if fs.NArg() != 0 {
		return errors.New("unexpected session arguments")
	}
	if dryRun && a.Apply {
		return errors.New("dry-run and apply cannot be combined")
	}
	if listAll {
		a.View = "all"
	}
	if archived {
		if listAll {
			return errors.New("all and archived cannot be combined")
		}
		a.View = "archived"
	}
	a.Targets = targets
	if messages != "" {
		a.Messages = strings.Split(messages, ",")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if op == "session/attach" {
		if e := nativeContext(&a); e != nil {
			return e
		}
		legacyWorker(home, &a)
	} else if op != "session/list" && op != "session/gc" {
		if e := currentSession(ctx, home, &a); e != nil {
			return e
		}
	}
	if op == "session/detach" {
		current, e := link.Call(ctx, home, "session/current", a)
		if e != nil {
			return e
		}
		var ep link.Endpoint
		if e = json.Unmarshal(current, &ep); e != nil {
			return e
		}
		if ep.Harness == "codex" {
			if e = observeCodexReceiver(ctx, current); e != nil {
				return e
			}
		}
	}
	raw, e := link.Call(ctx, home, op, a)
	if e != nil {
		return e
	}
	if op == "session/attach" && a.EnableWake && a.Harness == "codex" {
		// The installed native adapter validates the current native owner and installs
		// only this chat's sidecar. It never opens another chat or changes approvals.
		if e = enableCodexReceiver(ctx, raw); e != nil {
			return e
		}
		raw, e = link.Call(ctx, home, "session/current", a)
		if e != nil {
			return e
		}
	}
	var out any
	if e = json.Unmarshal(raw, &out); e != nil {
		return e
	}
	return printJSON(out)
}

func enableCodexReceiver(ctx context.Context, raw json.RawMessage) error {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	adapter := filepath.Join(filepath.Dir(filepath.Dir(exe)), "share", "agent-link", "adapters", "session-host.mjs")
	node, e := exec.LookPath("node")
	if e != nil {
		return errors.New("chat registered; Node runtime required to enable the native receiver")
	}
	cmd := exec.CommandContext(ctx, node, adapter, "attach-codex")
	cmd.Env = append(os.Environ(), "AGENT_LINK_CLI_PATH="+exe)
	cmd.Stdin = strings.NewReader(string(raw))
	out, e := cmd.CombinedOutput()
	if e != nil {
		return errors.New("chat registered; native receiver not enabled: " + strings.TrimSpace(string(out)))
	}
	return nil
}

func observeCodexReceiver(ctx context.Context, raw json.RawMessage) error {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	node, e := exec.LookPath("node")
	if e != nil {
		return e
	}
	adapter := filepath.Join(filepath.Dir(filepath.Dir(exe)), "share", "agent-link", "adapters", "session-host.mjs")
	cmd := exec.CommandContext(ctx, node, adapter, "observe-codex")
	cmd.Env = append(os.Environ(), "AGENT_LINK_CLI_PATH="+exe)
	cmd.Stdin = strings.NewReader(string(raw))
	out, e := cmd.CombinedOutput()
	if e != nil {
		return errors.New("native presence could not be checked: " + strings.TrimSpace(string(out)))
	}
	return nil
}
