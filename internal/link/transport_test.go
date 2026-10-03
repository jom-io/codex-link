package link

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func testConfig(name, id, addr string) Config {
	c := Config{Name: name, DeviceID: id, Workspace: "test"}
	c.Redis.Address = addr
	c.Redis.AllowInsecure = true
	c.Defaults()
	return c
}
func TestRedisEncryptedDurableDelivery(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()
	a := testConfig("mac-a", NewID(), mr.Addr())
	b := testConfig("mac-b", NewID(), mr.Addr())
	ta, e := NewTransport(a, Secrets{WorkspaceKey: testKey})
	if e != nil {
		t.Fatal(e)
	}
	defer ta.Close()
	tb, _ := NewTransport(b, Secrets{WorkspaceKey: testKey})
	defer tb.Close()
	if e = ta.Heartbeat(ctx); e != nil {
		t.Fatal(e)
	}
	tb.Heartbeat(ctx)
	id, e := ta.Resolve(ctx, "mac-b")
	if e != nil || id != b.DeviceID {
		t.Fatal(e)
	}
	m := sample("text")
	m.From = a.DeviceID
	m.To = b.DeviceID
	m.Text = "secret test payload"
	if e = ta.Publish(ctx, m); e != nil {
		t.Fatal(e)
	}
	if e = ta.Publish(ctx, m); e != nil {
		t.Fatal(e)
	}
	n, _ := ta.client.XLen(ctx, ta.Inbox(b.DeviceID)).Result()
	if n != 1 {
		t.Fatal("Redis retry duplicated stream entry")
	}
	raw, e := tb.Read(ctx, "0-0")
	if e != nil || len(raw) != 1 {
		t.Fatal("offline message missing", e)
	}
	got, e := tb.Decode(raw[0])
	if e != nil || got.Text != m.Text {
		t.Fatal(e)
	}
	if raw[0].Values["envelope"] == m.Text {
		t.Fatal("plaintext in Redis")
	}
	wrong, _ := NewTransport(b, Secrets{WorkspaceKey: testKey + "x"})
	defer wrong.Close()
	if _, e = wrong.Decode(raw[0]); e == nil {
		t.Fatal("wrong pairing key accepted")
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
func startTestDaemon(t *testing.T, home string, c Config) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunDaemonWith(ctx, home, c, Secrets{WorkspaceKey: testKey}) }()
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
func TestTwoDaemonTaskRoundTripAndRestart(t *testing.T) {
	mr := miniredis.RunT(t)
	a := testConfig("mac-a", NewID(), mr.Addr())
	b := testConfig("mac-b", NewID(), mr.Addr())
	ha, hb := shortHome(t), shortHome(t)
	stopA := startTestDaemon(t, ha, a)
	defer stopA()
	stopB := startTestDaemon(t, hb, b)
	ctx := context.Background()
	raw, e := Call(ctx, ha, "send", APIRequest{To: b.DeviceID, Kind: "task", Text: "check tool version", Session: "requester", Conversation: "setup"})
	if e != nil {
		stopB()
		t.Fatal(e)
	}
	var sent struct {
		ID string `json:"id"`
	}
	json.Unmarshal(raw, &sent)
	raw, e = Call(ctx, hb, "wait", APIRequest{After: 0, Timeout: 10, Conversation: "setup"})
	if e != nil {
		stopB()
		t.Fatal(e)
	}
	var records []Record
	json.Unmarshal(raw, &records)
	if len(records) != 1 || records[0].Message.ID != sent.ID {
		stopB()
		t.Fatal("remote task missing", string(raw))
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
	stopB()
	// Send while B is stopped, then restart with its existing SQLite cursor.
	if _, e = Call(ctx, ha, "send", APIRequest{To: b.DeviceID, Text: "offline message"}); e != nil {
		t.Fatal(e)
	}
	time.Sleep(1200 * time.Millisecond)
	stopB = startTestDaemon(t, hb, b)
	defer stopB()
	raw, e = Call(ctx, hb, "wait", APIRequest{After: records[0].Seq, Timeout: 10})
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
