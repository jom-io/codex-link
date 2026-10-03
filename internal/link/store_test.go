package link

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func sample(kind string) Message {
	return Message{ID: NewID(), Protocol: ProtocolVersion, From: "sender", To: "receiver", FromName: "mac-a", Kind: kind, Text: "test task", CreatedAt: time.Now().UTC()}
}
func TestStorePersistenceDedupAndTaskLease(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	s, e := OpenStore(home)
	if e != nil {
		t.Fatal(e)
	}
	m := sample("task")
	receipt := sample("receipt")
	receipt.ReplyTo = m.ID
	for _, cursor := range []string{"1-0", "2-0"} {
		if e = s.Accept(ctx, &m, cursor, &receipt); e != nil {
			t.Fatal(e)
		}
	}
	rs, e := s.List(ctx, 0, 100, false, "", "")
	if e != nil || len(rs) != 1 {
		t.Fatal(e, len(rs))
	}
	pending, _ := s.Pending(ctx)
	if len(pending) != 1 {
		t.Fatal("duplicate receipt queued")
	}
	s.Close()
	s, e = OpenStore(home)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	cursor, _ := s.Cursor(ctx)
	if cursor != "2-0" {
		t.Fatal("cursor not durable")
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	for _, owner := range []string{"chat-a", "chat-b", "chat-c"} {
		wg.Add(1)
		go func(o string) {
			defer wg.Done()
			if s.Claim(ctx, m.ID, o, 60) == nil {
				winners.Add(1)
			}
		}(owner)
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatal("multiple windows claimed task")
	}
	r, _ := s.Get(ctx, m.ID)
	reply := sample("result")
	if e = s.Complete(ctx, m.ID, "wrong-chat", "done", reply); e == nil {
		t.Fatal("wrong owner completed task")
	}
	if e = s.Complete(ctx, m.ID, r.Owner, "done", reply); e != nil {
		t.Fatal(e)
	}
	if e = s.Claim(ctx, m.ID, "new-chat", 60); e == nil {
		t.Fatal("completed task reclaimed")
	}
	r, _ = s.Get(ctx, m.ID)
	if r.Status != "completed" || r.Result != "done" {
		t.Fatal("result not persisted")
	}
	pending, _ = s.Pending(ctx)
	if len(pending) != 2 {
		t.Fatal("completion response not queued atomically")
	}
}
func TestSessionFilteringAndDeliveryReceipt(t *testing.T) {
	ctx := context.Background()
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m := sample("text")
	m.ToSession = "chat-a"
	s.Accept(ctx, &m, "1-0", nil)
	rs, _ := s.List(ctx, 0, 100, false, "chat-b", "")
	if len(rs) != 0 {
		t.Fatal("private session message exposed through filter")
	}
	rs, _ = s.List(ctx, 0, 100, false, "chat-a", "")
	if len(rs) != 1 {
		t.Fatal("session message missing")
	}
	out := sample("text")
	out.To = "peer"
	s.Enqueue(ctx, out)
	s.MarkSent(ctx, out.ID)
	receipt := sample("receipt")
	receipt.From = "imposter"
	receipt.ReplyTo = out.ID
	s.Accept(ctx, &receipt, "2-0", nil)
	r, _ := s.Get(ctx, out.ID)
	if r.Status != "sent" {
		t.Fatal("wrong peer acknowledged message")
	}
	receipt.ID = NewID()
	receipt.From = "peer"
	s.Accept(ctx, &receipt, "3-0", nil)
	r, _ = s.Get(ctx, out.ID)
	if r.Status != "delivered" {
		t.Fatal("delivery receipt ignored")
	}
}
