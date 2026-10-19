package wake

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jom-io/codex-link/internal/link"
)

type inboxFixture struct{ rows []link.Record }

func (f *inboxFixture) call(_ context.Context, op string, a link.APIRequest) (json.RawMessage, error) {
	var v any
	switch op {
	case "status":
		last := int64(0)
		for _, r := range f.rows {
			if r.Seq > last {
				last = r.Seq
			}
		}
		v = map[string]any{"device_id": "home", "last_seq": last}
	case "inbox":
		rows := []link.Record{}
		for _, r := range f.rows {
			if r.Seq > a.After {
				rows = append(rows, r)
				if len(rows) >= a.Limit {
					break
				}
			}
		}
		v = rows
	case "get":
		for _, r := range f.rows {
			if r.Message.ID == a.ID {
				b, _ := json.Marshal(r)
				return b, nil
			}
		}
		return nil, fmt.Errorf("not found")
	default:
		return nil, fmt.Errorf("unexpected operation")
	}
	b, e := json.Marshal(v)
	return b, e
}
func record(seq int64, kind string) link.Record {
	return link.Record{Seq: seq, Direction: "in", Status: "received", Message: link.Message{ID: fmt.Sprintf("m-%d", seq), From: "office", ToSession: "home-ds-test", Kind: kind, Text: "secret-bearing remote instruction must never enter the wake prompt"}}
}
func config(t *testing.T) Config {
	t.Helper()
	return Config{Session: "home-ds-test", NativeSession: "actual-chat-42", From: []string{"office", "home"}, After: 0, StatePath: filepath.Join(t.TempDir(), "queue.json"), Cooldown: time.Second}
}
func openFixture(t *testing.T, c Config, f *inboxFixture) *Watcher {
	t.Helper()
	w, e := Open(context.Background(), c, f.call)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

func TestExplicitBindingFilteringAndNoRemotePrompt(t *testing.T) {
	f := &inboxFixture{}
	for i, k := range []string{"task", "progress", "text", "result", "file", "task", "task", "task"} {
		f.rows = append(f.rows, record(int64(i+1), k))
	}
	f.rows[5].Message.ToSession = "other-chat"
	f.rows[6].Message.From = "another-device"
	f.rows[7].Message.ToSession = "" // broadcast must not wake every registered chat
	w := openFixture(t, config(t), f)
	calls := 0
	_, err := w.Step(context.Background(), func(_ context.Context, e Event) (Ack, error) {
		calls++
		if e.NativeSession != "actual-chat-42" || len(e.Messages) != 2 || e.Messages[0].Kind != "task" || e.Messages[1].Kind != "result" {
			t.Fatalf("wrong routing: %#v", e)
		}
		if strings.Contains(e.Prompt, "secret-bearing") {
			t.Fatal("remote body was promoted to user input")
		}
		if !strings.Contains(e.Prompt, "m-1") || !strings.Contains(e.Prompt, "existing user-approved scope") {
			t.Fatal("missing fixed retrieval context")
		}
		return Ack{ID: e.ID, Status: "accepted"}, nil
	}, time.Now())
	if err != nil || calls != 1 || len(w.state.Pending) != 0 || w.state.Cursor != 8 {
		t.Fatalf("%v %+v", err, w.state)
	}
	_, err = w.Step(context.Background(), func(context.Context, Event) (Ack, error) { t.Fatal("duplicate wake"); return Ack{}, nil }, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
}

func TestBusyQueueSurvivesRestartAndAdvancedCursor(t *testing.T) {
	f := &inboxFixture{rows: []link.Record{record(1, "task")}}
	c := config(t)
	w := openFixture(t, c, f)
	var eventID string
	_, err := w.Step(context.Background(), func(_ context.Context, e Event) (Ack, error) {
		eventID = e.ID
		return Ack{ID: e.ID, Status: "busy"}, nil
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if w.state.Cursor != 1 || len(w.state.Pending) != 1 {
		t.Fatal("busy inbox lost")
	}
	w.Close()
	w = openFixture(t, c, f)
	_, err = w.Step(context.Background(), func(_ context.Context, e Event) (Ack, error) {
		if e.ID != eventID {
			t.Fatal("restart changed notification identity")
		}
		return Ack{ID: e.ID, Status: "accepted"}, nil
	}, time.Now())
	if err != nil || len(w.state.Pending) != 0 {
		t.Fatalf("%v %+v", err, w.state)
	}
	info, _ := os.Stat(c.StatePath)
	if info.Mode().Perm() != 0600 {
		t.Fatal("queue is not private")
	}
}

func TestOtherOwnerCompletedTaskAndWaitingApproval(t *testing.T) {
	f := &inboxFixture{rows: []link.Record{record(1, "task"), record(2, "task"), record(3, "task")}}
	f.rows[0].Status = "completed"
	f.rows[1].Owner = "another-window"
	f.rows[1].LeaseUntil = time.Now().Add(time.Hour).Unix()
	w := openFixture(t, config(t), f)
	_, err := w.Step(context.Background(), func(_ context.Context, e Event) (Ack, error) {
		if len(e.Messages) != 1 || e.Messages[0].ID != "m-3" {
			t.Fatal(e.Messages)
		}
		return Ack{ID: e.ID, Status: "waiting_user"}, nil
	}, time.Now())
	if err != nil || len(w.state.Pending) != 1 || w.state.Status != "waiting_user" {
		t.Fatal(err, w.state)
	}
	f.rows[2].Owner = "a-new-owner"
	f.rows[2].LeaseUntil = time.Now().Add(time.Hour).Unix()
	_, err = w.Step(context.Background(), func(context.Context, Event) (Ack, error) {
		t.Fatal("claimed queued task still woke chat")
		return Ack{}, nil
	}, time.Now())
	if err != nil || len(w.state.Pending) != 0 {
		t.Fatal(err, w.state)
	}
}

func TestTailStartupRestartAndExclusiveBinding(t *testing.T) {
	f := &inboxFixture{rows: []link.Record{record(1, "task")}}
	c := config(t)
	c.After = -1
	w := openFixture(t, c, f)
	if w.state.Cursor != 1 {
		t.Fatal("old inbox would be autoexecuted on initial enable")
	}
	if _, e := Open(context.Background(), c, f.call); e == nil {
		t.Fatal("two listeners accepted one binding")
	}
	w.Close()
	c.NativeSession = "wrong-chat"
	if _, e := Open(context.Background(), c, f.call); e == nil {
		t.Fatal("cursor rebound to a different chat")
	}
}

func TestPaginationQueueLimitAndAcknowledgmentFailure(t *testing.T) {
	f := &inboxFixture{}
	for i := 1; i <= 300; i++ {
		f.rows = append(f.rows, record(int64(i), "task"))
	}
	w := openFixture(t, config(t), f)
	_, err := w.Step(context.Background(), func(_ context.Context, e Event) (Ack, error) { return Ack{ID: "forged", Status: "accepted"}, nil }, time.Now())
	if err == nil || len(w.state.Pending) != 256 || w.state.Cursor != 256 {
		t.Fatal("failed ACK lost queue or pagination", err, w.state.Cursor, len(w.state.Pending))
	}
	_, err = w.Step(context.Background(), func(_ context.Context, e Event) (Ack, error) {
		if len(e.Messages) != 20 {
			t.Fatal("unbounded batch")
		}
		return Ack{ID: e.ID, Status: "accepted"}, nil
	}, time.Now())
	if err != nil || len(w.state.Pending) != 236 {
		t.Fatal(err, len(w.state.Pending))
	}
	if err = w.Scan(context.Background()); err != nil || w.state.Cursor != 276 || len(w.state.Pending) != 256 {
		t.Fatal("unread messages were skipped", err, w.state.Cursor, len(w.state.Pending))
	}
}

func TestConversationAllowlistCooldownAndRateLimit(t *testing.T) {
	c := config(t)
	c.Conversations = []string{"authorized"}
	f := &inboxFixture{rows: []link.Record{record(1, "task")}}
	f.rows[0].Message.Conversation = "other"
	w := openFixture(t, c, f)
	now := time.Now()
	accept := func(_ context.Context, e Event) (Ack, error) { return Ack{ID: e.ID, Status: "accepted"}, nil }
	if _, e := w.Step(context.Background(), accept, now); e != nil || w.state.Accepted != 0 {
		t.Fatal("unauthorized conversation woke chat")
	}
	for i := 2; i <= 12; i++ {
		r := record(int64(i), "task")
		r.Message.Conversation = "authorized"
		f.rows = append(f.rows, r)
		if _, e := w.Step(context.Background(), accept, now.Add(time.Duration(i)*time.Second)); e != nil {
			t.Fatal(e)
		}
	}
	if w.state.Accepted != 10 || w.state.Status != "rate_limited" || len(w.state.Pending) != 1 {
		t.Fatal("automatic reply loop was not bounded", w.state)
	}
	if _, e := w.Step(context.Background(), accept, now.Add(6*time.Minute)); e != nil || w.state.Accepted != 11 {
		t.Fatal("rate limit never recovered", e, w.state)
	}
}
