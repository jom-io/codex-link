package link

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func testConfig(name, id, addr string) Config {
	c := Config{Name: name, DeviceID: id, Workspace: "test", PairingHeadless: true}
	c.Redis.Address = addr
	c.Redis.AllowInsecure = true
	c.Defaults()
	return c
}
func testDevice(name, addr string) (Config, Secrets) {
	s := Secrets{}
	i, e := EnsureIdentity(&s)
	if e != nil {
		panic(e)
	}
	return testConfig(name, i.ID(), addr), s
}
func TestRedisEncryptedDurableDelivery(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()
	a, sa := testDevice("mac-a", mr.Addr())
	b, sb := testDevice("mac-b", mr.Addr())
	ta, e := NewTransport(a, sa)
	if e != nil {
		t.Fatal(e)
	}
	defer ta.Close()
	tb, _ := NewTransport(b, sb)
	defer tb.Close()
	storeA, _ := OpenStore(t.TempDir())
	defer storeA.Close()
	storeB, _ := OpenStore(t.TempDir())
	defer storeB.Close()
	ta.store = storeA
	tb.store = storeB
	ta.Heartbeat(ctx)
	tb.Heartbeat(ctx)
	id, e := ta.Resolve(ctx, "mac-b")
	if e != nil || id != b.DeviceID {
		t.Fatal(e)
	}
	m := sample("text")
	m.From = a.DeviceID
	m.To = b.DeviceID
	m.Text = "secret test payload"
	if e = ta.Publish(ctx, m); !errors.Is(e, ErrNotPaired) {
		t.Fatal("unpaired message allowed", e)
	}
	nonce := NewID()
	storeA.SavePair(ctx, Pair{Peer: tb.LocalPeer(), Nonce: nonce, LocalApproved: true, RemoteApproved: true}, nil)
	storeB.SavePair(ctx, Pair{Peer: ta.LocalPeer(), Nonce: nonce, LocalApproved: true, RemoteApproved: true}, nil)
	ka, _ := ta.PairKey(ctx, b.DeviceID)
	kb, _ := tb.PairKey(ctx, a.DeviceID)
	if ka != kb {
		t.Fatal("automatic shared key derivation mismatch")
	}
	if e = ta.Publish(ctx, m); e != nil {
		t.Fatal(e)
	}
	if e = ta.Publish(ctx, m); e != nil {
		t.Fatal(e)
	}
	n, _ := ta.client.XLen(ctx, ta.Inbox(b.DeviceID)).Result()
	if n != 1 {
		t.Fatal("retry duplicated stream entry")
	}
	raw, e := tb.Read(ctx, "0-0")
	if e != nil || len(raw) != 1 {
		t.Fatal("offline message missing", e)
	}
	got, e := tb.Decode(raw[0])
	if e != nil || got.Text != m.Text {
		t.Fatal(e)
	}
	var wire Wire
	json.Unmarshal([]byte(raw[0].Values["envelope"].(string)), &wire)
	if wire.Plain {
		t.Fatal("application message sent in plaintext")
	}
	wire.Payload += "tampered"
	bad, _ := json.Marshal(wire)
	raw[0].Values["envelope"] = string(bad)
	if _, e = tb.Decode(raw[0]); e == nil {
		t.Fatal("invalid signature accepted")
	}
}
func shortHome(t *testing.T) string {
	t.Helper()
	p, e := os.MkdirTemp("/tmp", "cl-test-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(p) })
	return p
}
func startTestDaemon(t *testing.T, home string, c Config, s Secrets) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunDaemonWith(ctx, home, c, s) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		check, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
		_, e := Call(check, home, "status", APIRequest{})
		stop()
		if e == nil {
			break
		}
		select {
		case err := <-done:
			cancel()
			t.Fatal(err)
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("daemon start timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(6 * time.Second):
			t.Error("daemon shutdown timed out")
		}
	}
}
func awaitPair(t *testing.T, home string) Pair {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, e := Call(context.Background(), home, "pair/list", APIRequest{})
		if e != nil {
			t.Fatal(e)
		}
		var ps []Pair
		json.Unmarshal(raw, &ps)
		if len(ps) > 0 {
			return ps[0]
		}
		if time.Now().After(deadline) {
			t.Fatal("pairing request did not arrive")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
func TestTwoDaemonMutualPairingTaskAndRestart(t *testing.T) {
	mr := miniredis.RunT(t)
	a, sa := testDevice("mac-a", mr.Addr())
	b, sb := testDevice("mac-b", mr.Addr())
	ha, hb := shortHome(t), shortHome(t)
	stopA := startTestDaemon(t, ha, a, sa)
	defer stopA()
	stopB := startTestDaemon(t, hb, b, sb)
	ctx := context.Background()
	time.Sleep(100 * time.Millisecond)
	raw, e := Call(ctx, ha, "send", APIRequest{To: b.DeviceID, Kind: "task", Text: "check tool version", Session: "requester", Conversation: "setup"})
	if e != nil {
		stopB()
		t.Fatal(e)
	}
	var sent struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	json.Unmarshal(raw, &sent)
	if sent.Status != "pairing_pending" {
		stopB()
		t.Fatal("first contact did not require pairing")
	}
	pa, pb := awaitPair(t, ha), awaitPair(t, hb)
	if pa.Code != pb.Code || pa.ID != pb.ID {
		stopB()
		t.Fatal("pair codes differ")
	}
	if _, e = Call(ctx, ha, "pair/accept", APIRequest{ID: pa.ID, Code: "wrong"}); e == nil {
		stopB()
		t.Fatal("incorrect code accepted")
	}
	if _, e = Call(ctx, ha, "pair/accept", APIRequest{ID: pa.ID, Code: pa.Code}); e != nil {
		stopB()
		t.Fatal(e)
	}
	time.Sleep(1200 * time.Millisecond)
	raw, _ = Call(ctx, hb, "inbox", APIRequest{})
	var records []Record
	json.Unmarshal(raw, &records)
	if len(records) != 0 {
		stopB()
		t.Fatal("message delivered before bilateral confirmation")
	}
	if _, e = Call(ctx, hb, "pair/accept", APIRequest{ID: pb.ID, Code: pb.Code}); e != nil {
		stopB()
		t.Fatal(e)
	}
	raw, e = Call(ctx, hb, "wait", APIRequest{Timeout: 10, Conversation: "setup"})
	if e != nil {
		stopB()
		t.Fatal(e)
	}
	json.Unmarshal(raw, &records)
	if len(records) != 1 || records[0].Message.ID != sent.ID {
		stopB()
		t.Fatal("queued task missing after pairing", string(raw))
	}
	if _, e = Call(ctx, hb, "task/claim", APIRequest{ID: sent.ID, Session: "worker", Lease: 60}); e != nil {
		stopB()
		t.Fatal(e)
	}
	if _, e = Call(ctx, hb, "task/complete", APIRequest{ID: sent.ID, Session: "worker", Text: "version verified"}); e != nil {
		stopB()
		t.Fatal(e)
	}
	raw, e = Call(ctx, ha, "wait", APIRequest{Timeout: 10, Session: "requester", Conversation: "setup"})
	if e != nil {
		stopB()
		t.Fatal(e)
	}
	json.Unmarshal(raw, &records)
	if len(records) != 1 || records[0].Message.Kind != "result" {
		stopB()
		t.Fatal("result not returned", string(raw))
	}
	raw, _ = Call(ctx, hb, "status", APIRequest{})
	var status struct {
		LastSeq int64 `json:"last_seq"`
	}
	json.Unmarshal(raw, &status)
	stopB()
	if _, e = Call(ctx, ha, "send", APIRequest{To: b.DeviceID, Text: "offline message"}); e != nil {
		t.Fatal(e)
	}
	time.Sleep(1200 * time.Millisecond)
	stopB = startTestDaemon(t, hb, b, sb)
	defer stopB()
	raw, e = Call(ctx, hb, "wait", APIRequest{After: status.LastSeq, Timeout: 10})
	if e != nil {
		t.Fatal(e)
	}
	json.Unmarshal(raw, &records)
	found := false
	for _, r := range records {
		if r.Message.Text == "offline message" {
			found = true
		}
	}
	if !found {
		t.Fatal("restart failed to recover offline message", string(raw))
	}
}
func TestPairingRejectAndThirdDeviceIsolation(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()
	c, s := testDevice("mac-a", mr.Addr())
	store, _ := OpenStore(t.TempDir())
	defer store.Close()
	tr, _ := NewTransport(c, s)
	defer tr.Close()
	tr.store = store
	d := &Daemon{Config: c, Store: store, Transport: tr, changed: make(chan struct{})}
	remote, remoteSecrets := testDevice("mac-b", mr.Addr())
	rt, _ := NewTransport(remote, remoteSecrets)
	defer rt.Close()
	rt.Heartbeat(ctx)
	p, e := d.ensurePair(ctx, remote.DeviceID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = d.decidePair(ctx, p.ID, p.Code, false); e != nil {
		t.Fatal(e)
	}
	if _, e = tr.PairKey(ctx, remote.DeviceID); !errors.Is(e, ErrNotPaired) {
		t.Fatal("rejected peer gained channel")
	}
	third, ss := testDevice("mac-c", mr.Addr())
	tt, _ := NewTransport(third, ss)
	defer tt.Close()
	tt.Heartbeat(ctx)
	p, e = d.ensurePair(ctx, third.DeviceID)
	if e != nil {
		t.Fatal(e)
	}
	if p.LocalApproved || p.RemoteApproved {
		t.Fatal("new device inherited prior trust")
	}
}
