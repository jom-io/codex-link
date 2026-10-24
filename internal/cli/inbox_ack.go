package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"strings"
	"time"

	"github.com/jom-io/codex-link/internal/link"
)

func serveInboxAck(home string, args []string) error {
	a := link.APIRequest{Profile: "default"}
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		a.Messages = append(a.Messages, args[0])
		args = args[1:]
	}
	fs := flag.NewFlagSet("inbox ack", flag.ContinueOnError)
	fs.StringVar(&a.Session, "session", "", "this chat's retained full worker; inferred from native context if omitted")
	fs.StringVar(&a.Profile, "profile", "default", "this host's configuration instance")
	fs.Bool("json", true, "JSON output")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() != 0 || len(a.Messages) < 1 || len(a.Messages) > 32 {
		return errors.New("usage: inbox ack MESSAGE_ID [MESSAGE_ID ...] [--session RETAINED_WORKER]; 1–32 handled IDs before flags")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if e := currentSession(ctx, home, &a); e != nil {
		return e
	}
	raw, e := link.Call(ctx, home, "inbox/ack", a)
	if e != nil {
		return e
	}
	var out any
	if e = json.Unmarshal(raw, &out); e != nil {
		return e
	}
	return printJSON(out)
}
