package link

import (
	"context"
	"reflect"
	"testing"
)

func TestInboxAckIsExplicitScopedAtomicAndPersistent(t *testing.T) {
	ctx := context.Background()
	d, peer := pairedRoutingDaemons(t, "127.0.0.1:1")
	one := attachTest(t, d, "ack-one")
	two := attachTest(t, d, "ack-two")
	third := attachTest(t, d, "ack-other")
	v, e := d.dispatch(ctx, "/v1/bind", APIRequest{Session: one.Session, Channel: "ack-dev", Targets: []string{two.ID}})
	if e != nil {
		t.Fatal(e)
	}
	binding := v.([]Binding)[0]
	deliverLocalTest(t, d)
	if _, e = d.dispatch(ctx, "/v1/bind", APIRequest{Session: two.Session, ID: binding.InviteID}); e != nil {
		t.Fatal(e)
	}
	deliverLocalTest(t, d)
	incoming := func(kind, target string) Message {
		m := d.base(d.Config.DeviceID, kind)
		m.Session, m.ToSession, m.Conversation, m.Text = two.Session, target, binding.Channel, "non-secret ack sample"
		if e := d.Store.Enqueue(ctx, m); e != nil {
			t.Fatal(e)
		}
		deliverLocalTest(t, d)
		return m
	}
	result := incoming("result", one.Session)
	foreign := incoming("result", third.Session)
	active := incoming("task", one.Session)
	before, _ := d.Store.Get(ctx, result.ID)
	if _, e = d.dispatch(ctx, "/v1/get", APIRequest{ID: result.ID}); e != nil {
		t.Fatal(e)
	}
	if _, e = d.dispatch(ctx, "/v1/inbox", APIRequest{Session: one.Session, Limit: 100}); e != nil {
		t.Fatal(e)
	}
	handled := func(session, id string) bool {
		t.Helper()
		v, e := d.dispatch(ctx, "/v1/inbox/handled", APIRequest{Session: session, ID: id})
		if e != nil {
			t.Fatal(e)
		}
		return v.(map[string]any)["handled"].(bool)
	}
	if handled(one.Session, result.ID) {
		t.Fatal("reading acknowledged an unprocessed result")
	}
	for _, ids := range [][]string{{result.ID, foreign.ID}, {result.ID, NewID()}, {active.ID}, {}} {
		if _, e = d.dispatch(ctx, "/v1/inbox/ack", APIRequest{Session: one.Session, Messages: ids}); e == nil {
			t.Fatal("invalid batch, foreign chat or unfinished task was acknowledged", ids)
		}
		if handled(one.Session, result.ID) || handled(one.Session, active.ID) {
			t.Fatal("failed batch partially acknowledged messages")
		}
	}
	out := d.base(peer.Config.DeviceID, "result")
	out.Session, out.ToSession, out.Text = one.Session, two.Session, "outgoing sample"
	if e = d.Store.Enqueue(ctx, out); e != nil {
		t.Fatal(e)
	}
	if _, e = d.dispatch(ctx, "/v1/inbox/ack", APIRequest{Session: one.Session, Messages: []string{out.ID}}); e == nil {
		t.Fatal("outgoing record was acknowledged as incoming")
	}
	policy, e := d.wakeEligible(ctx, APIRequest{Session: one.Session, NativeSession: one.NativeSession, ID: result.ID})
	if e != nil || !policy.(map[string]any)["allowed"].(bool) {
		t.Fatal("fresh result was not eligible", policy, e)
	}
	for attempt := 0; attempt < 2; attempt++ {
		v, e = d.dispatch(ctx, "/v1/inbox/ack", APIRequest{Session: one.Session, Messages: []string{result.ID, result.ID}})
		if e != nil || v.(map[string]any)["acknowledged"].(int64) != int64(1-attempt) {
			t.Fatal("ack was not idempotent", v, e)
		}
	}
	policy, e = d.wakeEligible(ctx, APIRequest{Session: one.Session, NativeSession: one.NativeSession, ID: result.ID})
	if e != nil || policy.(map[string]any)["allowed"].(bool) {
		t.Fatal("handled result still eligible", policy, e)
	}
	after, _ := d.Store.Get(ctx, result.ID)
	if !reflect.DeepEqual(before, after) || handled(third.Session, result.ID) || handled(third.Session, foreign.ID) {
		t.Fatal("ack changed history or another chat's handled state", before, after)
	}
	if _, e = d.dispatch(ctx, "/v1/task/claim", APIRequest{ID: active.ID, Session: one.Session, Lease: 60}); e != nil {
		t.Fatal(e)
	}
	if _, e = d.dispatch(ctx, "/v1/task/complete", APIRequest{ID: active.ID, Session: one.Session, Text: "finished"}); e != nil {
		t.Fatal(e)
	}
	if _, e = d.dispatch(ctx, "/v1/inbox/ack", APIRequest{Session: one.Session, Messages: []string{active.ID}}); e != nil {
		t.Fatal(e)
	}
	if e = d.Store.Close(); e != nil {
		t.Fatal(e)
	}
	d.Store, e = OpenStore(d.Home)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Store.Close()
	if !handled(one.Session, result.ID) || !handled(one.Session, active.ID) {
		t.Fatal("ack lost after database restart")
	}
}
