package link

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

func pairedRoutingDaemons(t *testing.T, address string) (*Daemon, *Daemon) {
	t.Helper()
	a, sa := testDevice("route-a", address)
	b, sb := testDevice("route-b", address)
	makeDaemon := func(c Config, secrets Secrets) *Daemon {
		home := shortHome(t)
		store, e := OpenStore(home)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { store.Close() })
		transport, e := NewTransport(c, secrets)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { transport.Close() })
		transport.store = store
		d := &Daemon{Home: home, Config: c, Store: store, Transport: transport, Files: newLocalFiles(c, home), changed: make(chan struct{})}
		_, loopback, _ := net.ParseCIDR("127.0.0.0/8")
		d.Direct = &directRouter{daemon: d, networks: []*net.IPNet{loopback}, allowLoopback: true}
		return d
	}
	da, db := makeDaemon(a, sa), makeDaemon(b, sb)
	nonce := NewID()
	for _, pair := range []struct{ local, remote *Daemon }{{da, db}, {db, da}} {
		e := pair.local.Store.SavePair(context.Background(), Pair{Peer: pair.remote.Transport.LocalPeer(), Nonce: nonce, LocalApproved: true, RemoteApproved: true}, nil)
		if e != nil {
			t.Fatal(e)
		}
	}
	return da, db
}

func cacheEndpoint(t *testing.T, from, to *Daemon, endpoint string) {
	t.Helper()
	to.Transport.endpoints = []string{endpoint}
	b, _ := json.Marshal(to.Transport.directAdvertisement())
	if e := from.Store.setMetadata(context.Background(), "direct-cache:"+to.Config.DeviceID, string(b)); e != nil {
		t.Fatal(e)
	}
}

func queued(t *testing.T, d *Daemon, op string, request APIRequest) Message {
	t.Helper()
	raw, e := d.dispatch(context.Background(), "/v1/"+op, request)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(raw)
	var response struct{ ID string }
	if e = json.Unmarshal(b, &response); e != nil || response.ID == "" {
		t.Fatal(e, string(b))
	}
	r, e := d.Store.Get(context.Background(), response.ID)
	if e != nil {
		t.Fatal(e)
	}
	return r.Message
}

func TestLocalTaskAndFileWithoutRedisOrOSS(t *testing.T) {
	c, secrets := testDevice("home-mac", "127.0.0.1:1")
	c.Routing = "cloud" // Local delivery still takes priority with LAN disabled.
	home := shortHome(t)
	stop := startTestDaemon(t, home, c, secrets)
	ctx := context.Background()
	raw, e := Call(ctx, home, "send", APIRequest{To: "home-mac", Kind: "task", Text: "local task", Session: "home-codex", ToSession: "home-ds", Conversation: "local"})
	if e != nil {
		stop()
		t.Fatal(e)
	}
	var sent struct{ ID string }
	json.Unmarshal(raw, &sent)
	raw, e = Call(ctx, home, "wait", APIRequest{Timeout: 3, Session: "home-ds", Conversation: "local"})
	var inbox []Record
	json.Unmarshal(raw, &inbox)
	if e != nil || len(inbox) != 1 || inbox[0].Message.ID != sent.ID || inbox[0].Route != "local" {
		stop()
		t.Fatal(e, string(raw))
	}
	if _, e = Call(ctx, home, "task/claim", APIRequest{ID: sent.ID, Session: "home-ds", Lease: 60}); e != nil {
		stop()
		t.Fatal(e)
	}
	if _, e = Call(ctx, home, "task/complete", APIRequest{ID: sent.ID, Session: "home-ds", Text: "local result"}); e != nil {
		stop()
		t.Fatal(e)
	}
	found := false
	after := inbox[0].Seq
	deadline := time.Now().Add(3 * time.Second)
	for !found && time.Now().Before(deadline) {
		raw, e = Call(ctx, home, "wait", APIRequest{After: after, Timeout: 1, Session: "home-codex", Conversation: "local"})
		json.Unmarshal(raw, &inbox)
		for _, record := range inbox {
			if record.Seq > after {
				after = record.Seq
			}
			if record.Message.Text == "local result" && record.Route == "local" {
				found = true
			}
		}
	}
	if e != nil || !found {
		stop()
		t.Fatal("local task result missing", e, string(raw))
	}
	source := filepath.Join(home, "source.txt")
	payload := []byte("local file / 中文 / no cloud")
	os.WriteFile(source, payload, 0600)
	raw, e = Call(ctx, home, "file/send", APIRequest{To: "self", Path: source, Conversation: "local-file"})
	if e != nil {
		stop()
		t.Fatal(e)
	}
	json.Unmarshal(raw, &sent)
	// The immutable snapshot, rather than this modified source, must arrive.
	os.WriteFile(source, []byte("changed after send"), 0600)
	raw, e = Call(ctx, home, "wait", APIRequest{Timeout: 3, Conversation: "local-file"})
	json.Unmarshal(raw, &inbox)
	if e != nil || len(inbox) != 1 || inbox[0].Route != "local" {
		stop()
		t.Fatal(e, string(raw))
	}
	stop()
	stop = startTestDaemon(t, home, c, secrets)
	defer stop()
	raw, e = Call(ctx, home, "file/fetch", APIRequest{ID: sent.ID})
	var fetched struct{ Path string }
	json.Unmarshal(raw, &fetched)
	b, readErr := os.ReadFile(fetched.Path)
	if e != nil || readErr != nil || !bytes.Equal(b, payload) {
		t.Fatal("local cache did not survive restart", e, readErr)
	}
}

func TestDirectTaskAndFileWithoutCloud(t *testing.T) {
	da, db := pairedRoutingDaemons(t, "127.0.0.1:1")
	sa, sb := httptest.NewServer(da.Direct), httptest.NewServer(db.Direct)
	defer sa.Close()
	defer sb.Close()
	cacheEndpoint(t, da, db, strings.TrimPrefix(sb.URL, "http://"))
	cacheEndpoint(t, db, da, strings.TrimPrefix(sa.URL, "http://"))
	ctx := context.Background()
	db.Store.Accept(ctx, nil, "123-0", nil)
	m := queued(t, da, "send", APIRequest{To: db.Config.DeviceID, Kind: "task", Text: "LAN task", Session: "office", ToSession: "home-ds"})
	if e := da.deliverOutgoing(ctx, m); e != nil {
		t.Fatal(e)
	}
	r, _ := da.Store.Get(ctx, m.ID)
	if r.Status != "delivered" || r.Route != "lan" {
		t.Fatal("unauthenticated or missing ACK", r)
	}
	if cursor, _ := db.Store.Cursor(ctx); cursor != "123-0" {
		t.Fatal("LAN reset Redis cursor", cursor)
	}
	if _, e := db.dispatch(ctx, "/v1/task/claim", APIRequest{ID: m.ID, Session: "home-ds", Lease: 60}); e != nil {
		t.Fatal(e)
	}
	if _, e := db.dispatch(ctx, "/v1/task/complete", APIRequest{ID: m.ID, Session: "home-ds", Text: "LAN done"}); e != nil {
		t.Fatal(e)
	}
	db.flushPending(ctx, false)
	inbox, _ := da.Store.List(ctx, 0, 100, false, "office", "")
	found := false
	for _, record := range inbox {
		if record.Message.Text == "LAN done" && record.Route == "lan" {
			found = true
		}
	}
	if !found {
		t.Fatal("LAN task completion not returned")
	}
	source := filepath.Join(da.Home, "selected.txt")
	payload := bytes.Repeat([]byte("LAN file / 中文\n"), 8000)
	os.WriteFile(source, payload, 0600)
	m = queued(t, da, "file/send", APIRequest{To: db.Config.DeviceID, Path: source})
	os.Remove(source)
	if e := da.deliverOutgoing(ctx, m); e != nil {
		t.Fatal(e)
	}
	raw, e := db.dispatch(ctx, "/v1/file/fetch", APIRequest{ID: m.ID})
	if e != nil {
		t.Fatal(e)
	}
	path := raw.(map[string]string)["path"]
	got, e := os.ReadFile(path)
	if e != nil || !bytes.Equal(got, payload) {
		t.Fatal("LAN file mismatch", e)
	}
	if _, e = os.Stat(da.Files.stagedPath(m.ID)); !os.IsNotExist(e) {
		t.Fatal("successful send retained staging file")
	}
	if r, _ = db.Store.Get(ctx, m.ID); r.Route != "lan" {
		t.Fatal("file did not use LAN")
	}
}

func TestDirectOnPrivateInterface(t *testing.T) {
	da, db := pairedRoutingDaemons(t, "127.0.0.1:1")
	ctx := context.Background()
	stopA, stopB := startDirect(ctx, da), startDirect(ctx, db)
	defer stopA()
	defer stopB()
	if len(da.Transport.endpoints) == 0 || len(db.Transport.endpoints) == 0 {
		t.Skip("no private IPv4 interface available")
	}
	cacheEndpoint(t, da, db, db.Transport.endpoints[0])
	m := queued(t, da, "send", APIRequest{To: db.Config.DeviceID, Text: "private-interface direct delivery"})
	if e := da.deliverOutgoing(ctx, m); e != nil {
		t.Fatal(e)
	}
	r, e := da.Store.Get(ctx, m.ID)
	if e != nil || r.Status != "delivered" || r.Route != "lan" {
		t.Fatal("private-interface direct route not acknowledged", e)
	}
}

func routingOSS(t *testing.T, da, db *Daemon) func() (int, map[string][]byte) {
	t.Helper()
	objects := map[string][]byte{}
	var mu sync.Mutex
	puts := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case "PUT":
			puts++
			objects[r.URL.Path], _ = io.ReadAll(r.Body)
		case "GET":
			b, ok := objects[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write(b)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)
	client, e := oss.New(server.URL, "test-id", "test-secret", oss.UseCname(true), oss.HTTPClient(server.Client()), oss.AuthVersion(oss.AuthV4), oss.Region("cn-hangzhou"))
	if e != nil {
		t.Fatal(e)
	}
	bucket, e := client.Bucket("test-bucket")
	if e != nil {
		t.Fatal(e)
	}
	da.Files.bucket, db.Files.bucket = bucket, bucket
	return func() (int, map[string][]byte) {
		mu.Lock()
		defer mu.Unlock()
		copy := make(map[string][]byte)
		for key, value := range objects {
			copy[key] = append([]byte(nil), value...)
		}
		return puts, copy
	}
}

func TestLostDirectACKFallsBackWithoutDuplicateTaskOrFile(t *testing.T) {
	mr := miniredis.RunT(t)
	da, db := pairedRoutingDaemons(t, mr.Addr())
	snapshot := routingOSS(t, da, db)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Commit the receiver's file/message, then lose the authenticated ACK.
		recorder := httptest.NewRecorder()
		db.Direct.ServeHTTP(recorder, r)
		if recorder.Code != http.StatusOK {
			t.Errorf("direct receive failed: %d %s", recorder.Code, recorder.Body.String())
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cacheEndpoint(t, da, db, strings.TrimPrefix(server.URL, "http://"))
	ctx := context.Background()
	for _, file := range []bool{false, true} {
		var m Message
		if file {
			path := filepath.Join(da.Home, "fallback.txt")
			os.WriteFile(path, []byte("cloud fallback file"), 0600)
			m = queued(t, da, "file/send", APIRequest{To: db.Config.DeviceID, Path: path})
		} else {
			m = queued(t, da, "send", APIRequest{To: db.Config.DeviceID, Kind: "task", Text: "once only"})
		}
		if e := da.deliverOutgoing(ctx, m); e != nil {
			t.Fatal(e)
		}
		out, _ := da.Store.Get(ctx, m.ID)
		if out.Route != "cloud" || out.Status != "sent" {
			t.Fatal("cloud fallback not used", out)
		}
		raw, e := db.Transport.Read(ctx, "0-0")
		if e != nil {
			t.Fatal(e)
		}
		for _, entry := range raw {
			incoming, e := db.Transport.Decode(entry)
			if e != nil {
				t.Fatal(e)
			}
			receipt := db.base(incoming.From, "receipt")
			receipt.ReplyTo = incoming.ID
			if e = db.acceptIncoming(ctx, incoming, entry.ID, &receipt, "cloud"); e != nil {
				t.Fatal(e)
			}
			if incoming.ID == m.ID {
				if e = da.Store.Accept(ctx, &receipt, "", nil); e != nil {
					t.Fatal(e)
				}
			}
		}
		inbox, _ := db.Store.List(ctx, 0, 100, false, "", "")
		count := 0
		for _, record := range inbox {
			if record.Message.ID == m.ID {
				count++
			}
		}
		if count != 1 {
			t.Fatal("fallback duplicated delivery", count)
		}
		out, _ = da.Store.Get(ctx, m.ID)
		if out.Status != "delivered" {
			t.Fatal("duplicate cloud delivery was not acknowledged")
		}
		if file {
			puts, objects := snapshot()
			if puts != 1 || len(objects) != 1 {
				t.Fatal("missing or duplicate OSS upload")
			}
			for _, ciphertext := range objects {
				if bytes.Contains(ciphertext, []byte("cloud fallback file")) {
					t.Fatal("plaintext uploaded")
				}
			}
			// Force an actual OSS download rather than reusing the LAN cache.
			path, _ := db.Files.cachePath(m)
			os.Remove(path)
			key, _ := db.fileKey(ctx, m.From)
			path, e = db.Files.Download(ctx, m, key)
			if e != nil {
				t.Fatal(e)
			}
			b, _ := os.ReadFile(path)
			if string(b) != "cloud fallback file" {
				t.Fatal("fallback file mismatch")
			}
		}
	}
}

func TestDirectDiscoveryRejectsForgeryAndForeignAddresses(t *testing.T) {
	da, db := pairedRoutingDaemons(t, "127.0.0.1:1")
	cacheEndpoint(t, da, db, "127.0.0.1:12345")
	ctx := context.Background()
	b := []byte(da.Store.metadata(ctx, "direct-cache:"+db.Config.DeviceID))
	var ad directAdvertisement
	json.Unmarshal(b, &ad)
	ad.Endpoints = []string{"192.168.1.2:80"}
	forged, _ := json.Marshal(ad)
	if _, ok := da.Transport.verifyDirect(ctx, db.Config.DeviceID, forged); ok {
		t.Fatal("forged endpoint accepted")
	}
	ad = db.Transport.directAdvertisement()
	ad.Scope = "other-workspace"
	ad.Signature = db.Transport.identity.Sign(ad.signedBytes())
	forged, _ = json.Marshal(ad)
	if _, ok := da.Transport.verifyDirect(ctx, db.Config.DeviceID, forged); ok {
		t.Fatal("foreign workspace accepted")
	}
	ad = db.Transport.directAdvertisement()
	ad.SeenAt = time.Now().Add(-8 * 24 * time.Hour)
	ad.Signature = db.Transport.identity.Sign(ad.signedBytes())
	forged, _ = json.Marshal(ad)
	if _, ok := da.Transport.verifyDirect(ctx, db.Config.DeviceID, forged); ok {
		t.Fatal("expired advertisement accepted")
	}
	for _, endpoint := range []string{"example.com:80", "8.8.8.8:80", "192.168.1.2:80", "127.0.0.1:0", "127.0.0.1:70000"} {
		if da.Direct.allowedEndpoint(endpoint) {
			t.Fatal("foreign or invalid endpoint allowed", endpoint)
		}
	}
	// The old Peer remains byte-compatible; direct metadata is not added to it.
	peer := db.Transport.LocalPeer()
	if !peer.Verify() {
		t.Fatal("legacy peer signature changed")
	}
}

func TestDirectRejectsTamperedFileAndForgedACK(t *testing.T) {
	da, db := pairedRoutingDaemons(t, "127.0.0.1:1")
	server := httptest.NewServer(db.Direct)
	defer server.Close()
	cacheEndpoint(t, da, db, strings.TrimPrefix(server.URL, "http://"))
	source := filepath.Join(da.Home, "tamper.txt")
	os.WriteFile(source, []byte("authenticated file"), 0600)
	m := queued(t, da, "file/send", APIRequest{To: db.Config.DeviceID, Path: source})
	staged, _ := os.ReadFile(da.Files.stagedPath(m.ID))
	staged[len(staged)-1] ^= 1
	os.WriteFile(da.Files.stagedPath(m.ID), staged, 0600)
	if e := da.Direct.send(context.Background(), m); e == nil {
		t.Fatal("tampered file accepted")
	}
	if _, e := db.Store.Get(context.Background(), m.ID); e == nil {
		t.Fatal("partial file message published")
	}
	if path, _ := db.Files.cachePath(m); path != "" {
		if _, e := os.Stat(path); !os.IsNotExist(e) {
			t.Fatal("partial file published")
		}
	}
	forged := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Write([]byte(`{"kind":"receipt","reply_to":"fake"}`))
	}))
	defer forged.Close()
	cacheEndpoint(t, da, db, strings.TrimPrefix(forged.URL, "http://"))
	m = queued(t, da, "send", APIRequest{To: db.Config.DeviceID, Text: "must be acknowledged by pinned peer"})
	if e := da.Direct.send(context.Background(), m); e == nil {
		t.Fatal("unsigned ACK accepted")
	}
	r, _ := da.Store.Get(context.Background(), m.ID)
	if r.Status != "pending" {
		t.Fatal("forged ACK changed status")
	}
}

func TestStagedCloudRetrySurvivesRestartWithoutReupload(t *testing.T) {
	mr := miniredis.RunT(t)
	da, db := pairedRoutingDaemons(t, mr.Addr())
	da.Direct = nil
	snapshot := routingOSS(t, da, db)
	source := filepath.Join(da.Home, "retry.txt")
	os.WriteFile(source, []byte("one immutable upload"), 0600)
	m := queued(t, da, "file/send", APIRequest{To: db.Config.DeviceID, Path: source})
	os.Remove(source)
	mr.SetError("simulated Redis outage")
	if e := da.deliverOutgoing(context.Background(), m); e == nil {
		t.Fatal("Redis outage hidden")
	}
	if da.Store.metadata(context.Background(), "staged-file:"+m.ID) != "uploaded" {
		t.Fatal("upload state not durable")
	}
	da.Store.Close()
	store, e := OpenStore(da.Home)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	da.Store, da.Transport.store = store, store
	mr.SetError("")
	if e = da.deliverOutgoing(context.Background(), m); e != nil {
		t.Fatal(e)
	}
	puts, _ := snapshot()
	if puts != 1 {
		t.Fatal("retry uploaded the same file again", puts)
	}
	r, _ := da.Store.Get(context.Background(), m.ID)
	if r.Status != "sent" || r.Route != "cloud" {
		t.Fatal("restart lost pending message")
	}
}

func TestFileCacheRejectsTraversal(t *testing.T) {
	da, db := pairedRoutingDaemons(t, "127.0.0.1:1")
	source := filepath.Join(da.Home, "selected.txt")
	os.WriteFile(source, []byte("selected content"), 0600)
	m := queued(t, da, "file/send", APIRequest{To: db.Config.DeviceID, Path: source})
	for _, id := range []string{".", "..", "../outside", ""} {
		bad := m
		bad.ID = id
		if _, e := db.Files.cachePath(bad); e == nil {
			t.Fatal("unsafe message cache ID accepted", id)
		}
	}
	bad := m
	copy := *m.File
	bad.File = &copy
	bad.File.Object = db.Files.root + db.Config.DeviceID + "/.."
	if _, e := db.Files.cachePath(bad); e == nil {
		t.Fatal("unsafe object suffix accepted")
	}
}
