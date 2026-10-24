package link

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func directoryPagesTest(t *testing.T, from, to *Daemon) []Message {
	t.Helper()
	ctx := context.Background()
	q, e := from.control(ctx, to.Config.DeviceID, "", "", sessionControl{Op: "query"})
	if e != nil {
		t.Fatal(e)
	}
	if e = from.Store.setMetadata(ctx, "directory-latest:"+to.Config.DeviceID, q.ID); e != nil {
		t.Fatal(e)
	}
	if e = to.receiveControl(ctx, q); e != nil {
		t.Fatal(e)
	}
	rows, e := to.Store.Pending(ctx)
	if e != nil {
		t.Fatal(e)
	}
	out := []Message{}
	for _, r := range rows {
		c, e := parseControl(r.Message)
		if e == nil && c.Request == q.ID && c.Op == "directory_page" {
			out = append(out, r.Message)
		}
	}
	return out
}
func TestDirectoryFullSnapshotPagingWithdrawalAndOldResponse(t *testing.T) {
	ctx := context.Background()
	a, b := pairedRoutingDaemons(t, "127.0.0.1:1")
	eps := []Endpoint{}
	for i := 0; i < 70; i++ {
		ep := attachTest(t, b, NewID())
		presenceTest(t, b, ep, "idle")
		eps = append(eps, ep)
	}
	pages := directoryPagesTest(t, a, b)
	if len(pages) != 2 {
		t.Fatal("directory silently truncated at 64", len(pages))
	}
	if e := a.receiveControl(ctx, pages[1]); e != nil {
		t.Fatal(e)
	}
	all, _ := a.Store.Endpoints(ctx)
	if len(all) != 0 {
		t.Fatal("partial snapshot applied", len(all))
	}
	if e := a.receiveControl(ctx, pages[0]); e != nil {
		t.Fatal(e)
	}
	all, _ = a.Store.Endpoints(ctx)
	if len(all) != 70 {
		t.Fatal("complete snapshot incomplete", len(all))
	}
	for _, ep := range all {
		if ep.Local || ep.NativeSession != "" || ep.Status != "online" || ep.FetchedAt.IsZero() {
			t.Fatal(ep)
		}
	}
	// An archived peer disappears only after the next complete authenticated view.
	old := eps[0]
	old.ArchivedAt = time.Now().UTC()
	old.ArchiveReason = "fixture-manual"
	if e := b.Store.saveValue(ctx, endpointKey(old.Device, old.Session), old); e != nil {
		t.Fatal(e)
	}
	next := directoryPagesTest(t, a, b)
	if e := a.receiveControl(ctx, next[0]); e != nil {
		t.Fatal(e)
	}
	cached, _ := a.Store.Endpoint(ctx, old.Device, old.Session)
	if cached.CacheRemoved {
		t.Fatal("missing page withdrew peer")
	}
	if e := a.receiveControl(ctx, next[1]); e != nil {
		t.Fatal(e)
	}
	cached, _ = a.Store.Endpoint(ctx, old.Device, old.Session)
	if !cached.CacheRemoved {
		t.Fatal("completed snapshot kept withdrawn discovery entry")
	}
	before := cached
	if e := a.receiveControl(ctx, pages[0]); e != nil {
		t.Fatal(e)
	}
	cached, _ = a.Store.Endpoint(ctx, old.Device, old.Session)
	if !reflect.DeepEqual(before, cached) {
		t.Fatal("old snapshot revived withdrawn peer")
	}
	v, e := a.listSessions(ctx, APIRequest{View: "active"})
	if e != nil || len(v) != 69 {
		t.Fatal("active directory includes withdrawn", len(v), e)
	}
	after := time.Now().Add(31 * 24 * time.Hour)
	a.lifecycleNow = func() time.Time { return after }
	v, e = a.listSessions(ctx, APIRequest{View: "active"})
	if e != nil || len(v) != 0 {
		t.Fatal("expired cached online endpoints stayed current", len(v), e)
	}
}
func TestLegacyControlJSONOmitsAllLifecycleFields(t *testing.T) {
	d, _, ep := lifecycleFixture(t)
	_ = d
	c := sessionControl{Op: "invite", Source: func() *Endpoint { v := publicEndpoint(ep); return &v }()}
	b, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	var wire map[string]json.RawMessage
	if e = json.Unmarshal(b, &wire); e != nil {
		t.Fatal(e)
	}
	var source map[string]json.RawMessage
	if e = json.Unmarshal(wire["source"], &source); e != nil {
		t.Fatal(e)
	}
	for _, key := range lifecycleFields {
		if _, ok := source[key]; ok {
			t.Fatal("legacy peer would reject new field", key)
		}
	}
	m := Message{Kind: "progress", Text: controlPrefix + string(b), From: ep.Device}
	if _, e = parseControl(m); e != nil {
		t.Fatal(e)
	}
}
