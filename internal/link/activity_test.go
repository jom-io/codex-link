package link

import (
	"context"
	"testing"
	"time"
)

func TestActivityTimeoutRecoveryAndOrdering(t *testing.T) {
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	a := Activity{Worker: "window-a", TaskID: NewID(), State: "working", SeenAt: time.Now().Add(-200 * time.Second)}
	if e = s.SaveActivity(ctx, "device", a); e != nil {
		t.Fatal(e)
	}
	v, e := s.Activities(ctx, a.TaskID)
	if e != nil || len(v) != 1 || v[0].Health != "unresponsive" {
		t.Fatal(v, e)
	}
	older := a
	older.SeenAt = older.SeenAt.Add(-time.Second)
	older.State = "blocked"
	s.SaveActivity(ctx, "device", older)
	v, _ = s.Activities(ctx, a.TaskID)
	if v[0].State != "working" {
		t.Fatal("out of order overwrote activity")
	}
	a.SeenAt = time.Now()
	s.SaveActivity(ctx, "device", a)
	v, _ = s.Activities(ctx, a.TaskID)
	if v[0].Health != "active" {
		t.Fatal("recovery missing")
	}
	a.State = "completed"
	a.SeenAt = time.Now()
	s.SaveActivity(ctx, "device", a)
	v, _ = s.Activities(ctx, a.TaskID)
	if v[0].Health != "completed" {
		t.Fatal(v)
	}
}

func TestWorkerPulseRequiresOwnerAndDoesNotRenew(t *testing.T) {
	ctx := context.Background()
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m := sample("task")
	s.Accept(ctx, &m, "1-0", nil)
	s.Claim(ctx, m.ID, "worker-a", 60)
	r, _ := s.Get(ctx, m.ID)
	d := Daemon{Config: Config{DeviceID: "receiver", Name: "mac-b"}, Store: s, changed: make(chan struct{})}
	if _, e = d.dispatch(ctx, "/v1/worker/pulse", APIRequest{ID: m.ID, Session: "worker-b", State: "working"}); e == nil {
		t.Fatal("wrong window pulsed")
	}
	if _, e = d.dispatch(ctx, "/v1/worker/pulse", APIRequest{ID: m.ID, Session: "worker-a", State: "blocked", Stage: "awaiting approval"}); e != nil {
		t.Fatal(e)
	}
	next, _ := s.Get(ctx, m.ID)
	if next.LeaseUntil != r.LeaseUntil {
		t.Fatal("pulse renewed lease")
	}
	v, _ := s.Activities(ctx, m.ID)
	if len(v) != 1 || v[0].State != "blocked" {
		t.Fatal(v)
	}
	d.checkActivity(ctx)
	pending, _ := s.Pending(ctx)
	n := len(pending)
	d.checkActivity(ctx)
	pending, _ = s.Pending(ctx)
	if len(pending) != n {
		t.Fatal("transition duplicated")
	}
}
