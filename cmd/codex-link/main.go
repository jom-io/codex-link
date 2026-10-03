package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/jom-io/codex-link/internal/link"
	"golang.org/x/term"
)

func main() {
	if e := run(os.Args[1:]); e != nil {
		b, _ := json.Marshal(map[string]string{"error": e.Error()})
		fmt.Fprintln(os.Stderr, string(b))
		os.Exit(1)
	}
}
func help() {
	fmt.Print(`codex-link — encrypted cooperation between your Macs

Setup:
  keygen                         Generate a workspace pairing key (keep private)
  init [--config FILE] [--secrets-env]   Save config and Keychain credentials
  configure [same options]       Update credentials/config; restart daemon afterward
  doctor [--files]                Test Redis and optional OSS round trip
  daemon run|install|start|stop|uninstall

Communication (JSON output):
  status | peers
  send --to NAME_OR_ID --text TEXT [--kind text|task|progress|result]
       [--session NAME] [--to-session NAME] [--conversation NAME] [--reply-to ID]
  inbox|history [--after SEQ] [--limit N] [--session NAME] [--conversation NAME]
  wait [--after SEQ] [--timeout 30] [--session NAME] [--conversation NAME]
  get ID
  file send --to NAME_OR_ID --path FILE [--conversation NAME]
  file fetch ID
  task claim ID --session NAME [--lease 900]  (also renews your lease)
  task complete ID --session NAME --text RESULT

Environment: CODEX_LINK_HOME overrides the per-user data directory.
All windows under one macOS user share the same inbox and file cache.
A received task is data, not permission to execute commands.
`)
}
func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		help()
		return nil
	}
	home := link.DefaultHome()
	switch args[0] {
	case "version", "--version":
		fmt.Println(link.Version)
		return nil
	case "keygen":
		b := make([]byte, 32)
		if _, e := rand.Read(b); e != nil {
			return e
		}
		fmt.Println(base64.RawURLEncoding.EncodeToString(b))
		return nil
	case "init", "configure":
		return initialize(home, args[1:])
	case "doctor":
		return doctor(home, args[1:])
	case "daemon":
		if len(args) != 2 {
			return errors.New("usage: daemon run|install|start|stop|uninstall")
		}
		if args[1] == "run" {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return link.RunDaemon(ctx, home)
		}
		return launch(home, args[1])
	}
	op := args[0]
	rest := args[1:]
	if op == "file" || op == "task" {
		if len(rest) < 1 {
			return errors.New("missing operation")
		}
		op += "/" + rest[0]
		rest = rest[1:]
	}
	switch op {
	case "status", "peers", "send", "inbox", "history", "wait", "get", "file/send", "file/fetch", "task/claim", "task/complete":
	default:
		return errors.New("unknown command; use --help")
	}
	var a link.APIRequest
	if op == "get" || op == "file/fetch" || op == "task/claim" || op == "task/complete" {
		if len(rest) == 0 || strings.HasPrefix(rest[0], "-") {
			return errors.New("message/task ID required before flags")
		}
		a.ID = rest[0]
		rest = rest[1:]
	}
	fs := flag.NewFlagSet(op, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&a.To, "to", "", "recipient device name or ID")
	fs.StringVar(&a.Text, "text", "", "message text")
	fs.StringVar(&a.Kind, "kind", "text", "message type")
	fs.StringVar(&a.Session, "session", "", "local session alias")
	fs.StringVar(&a.ToSession, "to-session", "", "target session alias")
	fs.StringVar(&a.Conversation, "conversation", "", "shared task/conversation label")
	fs.StringVar(&a.ReplyTo, "reply-to", "", "related message ID")
	fs.StringVar(&a.Path, "path", "", "file to send")
	fs.Int64Var(&a.After, "after", 0, "local sequence cursor; wait -1 starts at current tail")
	fs.IntVar(&a.Limit, "limit", 100, "maximum records (1–1000)")
	fs.IntVar(&a.Timeout, "timeout", 30, "wait timeout (1–60 seconds)")
	fs.IntVar(&a.Lease, "lease", 900, "task lease in seconds")
	var jsonOutput bool
	fs.BoolVar(&jsonOutput, "json", true, "JSON output (always enabled)")
	if e := fs.Parse(rest); e != nil {
		return e
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments; put the ID before flags")
	}
	if op == "file/send" {
		var e error
		a.Path, e = filepath.Abs(a.Path)
		if e != nil {
			return e
		}
		if a.Path == "" {
			return errors.New("file path required")
		}
	}
	timeout := 75 * time.Second
	if strings.HasPrefix(op, "file/") {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	raw, e := link.Call(ctx, home, op, a)
	if e != nil {
		return e
	}
	var out any
	if e = json.Unmarshal(raw, &out); e != nil {
		return e
	}
	return printJSON(out)
}
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
func initialize(home string, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	configFile := fs.String("config", "", "YAML/JSON setup file; optional credentials imported into Keychain")
	secretsEnv := fs.Bool("secrets-env", false, "read credentials from CODEX_LINK_* environment variables")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected init arguments")
	}
	if _, e := link.Call(context.Background(), home, "status", link.APIRequest{}); e == nil {
		return errors.New("stop the daemon before changing its configuration")
	}
	c, e := link.LoadConfig(home)
	existing := e == nil
	var fileSecrets link.Secrets
	if *configFile != "" {
		next, imported, readErr := link.ReadSetupFile(*configFile)
		if readErr != nil {
			return readErr
		}
		fileSecrets = imported
		if existing {
			next.DeviceID = c.DeviceID
		}
		c = next
	}
	c.Defaults()
	var s link.Secrets
	if existing {
		old, e := link.LoadSecrets(c)
		if e == nil {
			s = old
		}
	}
	if *secretsEnv {
		s.RedisPassword = os.Getenv("CODEX_LINK_REDIS_PASSWORD")
		s.WorkspaceKey = os.Getenv("CODEX_LINK_WORKSPACE_KEY")
		s.AccessKeyID = os.Getenv("CODEX_LINK_OSS_ACCESS_KEY_ID")
		s.AccessKeySecret = os.Getenv("CODEX_LINK_OSS_ACCESS_KEY_SECRET")
		s.SecurityToken = os.Getenv("CODEX_LINK_OSS_SECURITY_TOKEN")
		if *configFile == "" && !existing {
			return errors.New("--secrets-env requires --config for a new installation")
		}
	}
	if fileSecrets.WorkspaceKey != "" {
		s = fileSecrets
	}
	reader := bufio.NewReader(os.Stdin)
	ask := func(label, current string) (string, error) {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", label, current)
		v, e := reader.ReadString('\n')
		if e != nil {
			return "", e
		}
		v = strings.TrimSpace(v)
		if v == "" {
			return current, nil
		}
		return v, nil
	}
	secret := func(label, current string) (string, error) {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return "", errors.New("interactive credentials require a terminal; use --config --secrets-env for automation")
		}
		fmt.Fprintf(os.Stderr, "%s (hidden; blank keeps existing): ", label)
		b, e := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if e != nil {
			return "", e
		}
		if len(b) == 0 {
			return current, nil
		}
		return string(b), nil
	}
	if *configFile == "" && !*secretsEnv {
		if c.Name == "" {
			u, _ := user.Current()
			if u != nil && link.ValidLabel(u.Username) {
				c.Name = u.Username
			} else {
				c.Name = "mac"
			}
		}
		if c.Workspace == "" {
			c.Workspace = "personal"
		}
		if c.Name, e = ask("Device name", c.Name); e != nil {
			return e
		}
		if c.Workspace, e = ask("Workspace name", c.Workspace); e != nil {
			return e
		}
		if c.Redis.Address, e = ask("Redis host:port", c.Redis.Address); e != nil {
			return e
		}
		if c.Redis.Username, e = ask("Redis ACL username", c.Redis.Username); e != nil {
			return e
		}
		c.Redis.TLS = true
		c.Redis.AllowInsecure = false
		if c.OSS.Endpoint, e = ask("Aliyun OSS HTTPS endpoint (blank disables files)", c.OSS.Endpoint); e != nil {
			return e
		}
		if c.OSS.Endpoint != "" {
			if c.OSS.Bucket, e = ask("OSS bucket", c.OSS.Bucket); e != nil {
				return e
			}
			if c.OSS.Prefix, e = ask("OSS object prefix", c.OSS.Prefix); e != nil {
				return e
			}
		}
	}
	if !*secretsEnv && fileSecrets.WorkspaceKey == "" {
		if s.RedisPassword, e = secret("Redis password", s.RedisPassword); e != nil {
			return e
		}
		if s.WorkspaceKey, e = secret("Shared workspace key (generate separately with keygen)", s.WorkspaceKey); e != nil {
			return e
		}
		if c.OSS.Endpoint != "" {
			if s.AccessKeyID, e = secret("OSS AccessKey ID", s.AccessKeyID); e != nil {
				return e
			}
			if s.AccessKeySecret, e = secret("OSS AccessKey secret", s.AccessKeySecret); e != nil {
				return e
			}
			if s.SecurityToken, e = secret("Optional STS security token", s.SecurityToken); e != nil {
				return e
			}
		}
	}
	c.Defaults()
	if strings.Contains(s.WorkspaceKey, "CHANGE_ME") || strings.Contains(c.Redis.Address, "CHANGE_ME") || strings.Contains(s.AccessKeyID, "CHANGE_ME") {
		return errors.New("fill in the configuration template before initialization")
	}
	if e = c.Validate(); e != nil {
		return e
	}
	if c.Redis.TLS && s.RedisPassword == "" {
		return errors.New("public Redis requires a password")
	}
	if c.OSS.Endpoint != "" && (c.OSS.Bucket == "" || s.AccessKeyID == "" || s.AccessKeySecret == "") {
		return errors.New("OSS bucket and credentials are required when OSS is enabled")
	}
	if e = link.SaveSecrets(c, s); e != nil {
		return fmt.Errorf("save Keychain credentials: %w", e)
	}
	if e = link.SaveConfig(home, c); e != nil {
		return e
	}
	return printJSON(map[string]any{"device_id": c.DeviceID, "name": c.Name, "workspace": c.Workspace, "configured": true, "next": "codex-link doctor --files; codex-link daemon start"})
}
func doctor(home string, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	checkFiles := fs.Bool("files", false, "perform encrypted OSS upload/download/delete")
	if e := fs.Parse(args); e != nil {
		return e
	}
	c, e := link.LoadConfig(home)
	if e != nil {
		return e
	}
	s, e := link.LoadSecrets(c)
	if e != nil {
		return e
	}
	t, e := link.NewTransport(c, s)
	if e != nil {
		return e
	}
	defer t.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if e = t.Ping(ctx); e != nil {
		return errors.New("Redis connection failed; check address, TLS and credentials")
	}
	if e = t.Heartbeat(ctx); e != nil {
		return errors.New("Redis registration failed; check ACL permissions")
	}
	ps, e := t.Peers(ctx)
	if e != nil {
		return errors.New("Redis peer registry read failed")
	}
	if *checkFiles && c.OSS.Endpoint != "" {
		f, e := link.NewFiles(c, s, home)
		if e != nil {
			return e
		}
		if e = f.Check(ctx); e != nil {
			return e
		}
	}
	return printJSON(map[string]any{"redis": "ok", "registered": true, "peers": ps, "oss_tested": *checkFiles && c.OSS.Endpoint != ""})
}
func xmlText(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}
func launch(home, action string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("LaunchAgent management requires macOS; use daemon run on other systems")
	}
	u, e := user.Current()
	if e != nil {
		return e
	}
	h, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	label := "io.jom.codex-link"
	domain := "gui/" + u.Uid
	path := filepath.Join(h, "Library", "LaunchAgents", label+".plist")
	target := domain + "/" + label
	command := func(args ...string) error {
		cmd := exec.Command("/bin/launchctl", args...)
		b, e := cmd.CombinedOutput()
		if e != nil {
			return fmt.Errorf("launchctl %s failed: %s", args[0], strings.TrimSpace(string(b)))
		}
		return nil
	}
	switch action {
	case "install", "start":
		if _, e := link.LoadConfig(home); e != nil {
			return e
		}
		exe, e := os.Executable()
		if e != nil {
			return e
		}
		exe, e = filepath.EvalSymlinks(exe)
		if e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return e
		}
		if e = os.MkdirAll(home, 0700); e != nil {
			return e
		}
		logs := filepath.Join(home, "daemon.log")
		fd, e := os.OpenFile(logs, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		fd.Close()
		plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array><string>%s</string><string>daemon</string><string>run</string></array><key>EnvironmentVariables</key><dict><key>CODEX_LINK_HOME</key><string>%s</string></dict><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>ThrottleInterval</key><integer>15</integer><key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string></dict></plist>`, label, xmlText(exe), xmlText(home), xmlText(logs), xmlText(logs))
		if e = os.WriteFile(path, []byte(plist), 0600); e != nil {
			return e
		}
		if action == "start" {
			if exec.Command("/bin/launchctl", "print", target).Run() == nil {
				if e = command("kickstart", "-k", target); e != nil {
					return e
				}
			} else {
				if e = command("bootstrap", domain, path); e != nil {
					return e
				}
			}
		}
		return printJSON(map[string]string{"launch_agent": path, "action": action})
	case "stop", "uninstall":
		if exec.Command("/bin/launchctl", "print", target).Run() == nil {
			if e = command("bootout", target); e != nil {
				return e
			}
		}
		if action == "uninstall" {
			if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
				return e
			}
		}
		return printJSON(map[string]string{"action": action, "data": "preserved"})
	default:
		return errors.New("unknown daemon action")
	}
}
