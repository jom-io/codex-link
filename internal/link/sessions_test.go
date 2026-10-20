package link

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func attachTest(t *testing.T, d *Daemon, native string) Endpoint {
	t.Helper()
	v, e := d.dispatch(context.Background(), "/v1/session/attach", APIRequest{Harness: "deepseek-harness", Profile: "default", NativeSession: native, EnableWake: true})
	if e != nil {
		t.Fatal(e)
	}
	return v.(Endpoint)
}
func transferTest(t *testing.T, from, to *Daemon) {
	t.Helper()
	ctx := context.Background()
	rows, e := from.Store.Pending(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range rows {
		if r.Message.To != to.Config.DeviceID {
			continue
		}
		if e = to.acceptIncoming(ctx, r.Message, "", nil, "lan"); e != nil {
			t.Fatal(e)
		}
		if e = from.Store.MarkSent(ctx, r.Message.ID); e != nil {
			t.Fatal(e)
		}
	}
}
func TestPersistentChatAliasesAndNoIdentityTakeover(t *testing.T) {
	ctx := context.Background()
	d, _ := pairedRoutingDaemons(t, "127.0.0.1:1")
	one := attachTest(t, d, "native-one")
	two := attachTest(t, d, "native-two")
	if one.Alias == two.Alias || one.Session == two.Session || !strings.Contains(one.Alias, "-ds-") {
		t.Fatal(one, two)
	}
	if _, e := d.dispatch(ctx, "/v1/session/pause", APIRequest{Session: one.Session}); e != nil {
		t.Fatal(e)
	}
	if e := d.Store.Close(); e != nil {
		t.Fatal(e)
	}
	var e error
	d.Store, e = OpenStore(d.Home)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Store.Close()
	resumed := attachTest(t, d, "native-one")
	if resumed.Session != one.Session || resumed.ID != one.ID || resumed.Alias != one.Alias || !resumed.Paused {
		t.Fatal("chat/alias/pause changed on restart", resumed)
	}
	_, e = d.dispatch(ctx, "/v1/session/attach", APIRequest{Harness: one.Harness, Profile: one.Profile, NativeSession: "native-new", Session: one.Session})
	if e == nil {
		t.Fatal("another native chat took worker")
	}
	_, e = d.dispatch(ctx, "/v1/session/attach", APIRequest{Harness: one.Harness, Profile: one.Profile, NativeSession: "native-one", Session: two.Session})
	if e == nil {
		t.Fatal("existing native chat replaced worker")
	}
	alias := defaultAlias("office-mac", "codex", "abcd1234", time.Date(2026, 10, 20, 3, 48, 0, 0, time.UTC))
	if alias != "office-codex-1020-1148-abcd" {
		t.Fatal(alias)
	}
}
func TestSessionBindingHandshakeFanoutScopeAndRealProbe(t *testing.T) {
	ctx := context.Background()
	a, b := pairedRoutingDaemons(t, "127.0.0.1:1")
	office := attachTest(t, a, "office-native")
	home1 := attachTest(t, b, "first-home-native")
	home2 := attachTest(t, b, "second-home-native")
	query, e := a.control(ctx, b.Config.DeviceID, "", "", sessionControl{Op: "query"})
	if e != nil {
		t.Fatal(e)
	}
	transferTest(t, a, b)
	transferTest(t, b, a)
	if a.Store.metadata(ctx, "directory-ready:"+b.Config.DeviceID) == "" {
		t.Fatal("directory handshake missing", query.ID)
	}
	all, _ := a.Store.Endpoints(ctx)
	remote := 0
	for _, ep := range all {
		if !ep.Local {
			remote++
			raw, _ := json.Marshal(ep)
			if strings.Contains(string(raw), "native") || ep.StatePath != "" || ep.Profile != "" {
				t.Fatal("native context leaked", string(raw))
			}
		}
	}
	if remote != 2 {
		t.Fatal(all)
	}
	// Legacy peers simply save progress and issue no capability response.
	legacy := a.base(b.Config.DeviceID, "progress")
	legacy.Text = controlPrefix + `{"op":"unknown-new-operation"}`
	if e = b.receiveControl(ctx, legacy); e != nil {
		t.Fatal(e)
	}
	v, e := a.dispatch(ctx, "/v1/bind", APIRequest{Session: office.Session, Channel: "starx-dev", Targets: []string{home1.ID, home2.ID}})
	if e != nil {
		t.Fatal(e)
	}
	bindings := v.([]Binding)
	if _, e = a.dispatch(ctx, "/v1/send", APIRequest{Session: office.Session, Channel: "starx-dev", To: home1.ID, Text: "too early"}); e == nil {
		t.Fatal("unaccepted peer was sent a channel task")
	}
	transferTest(t, a, b)
	if _, e = b.dispatch(ctx, "/v1/bind", APIRequest{Session: home2.Session, ID: bindings[0].InviteID}); e == nil {
		t.Fatal("invite accepted by wrong chat")
	}
	for i, ep := range []Endpoint{home1, home2} {
		if _, e = b.dispatch(ctx, "/v1/bind", APIRequest{Session: ep.Session, ID: bindings[i].InviteID}); e != nil {
			t.Fatal(e)
		}
	}
	transferTest(t, b, a)
	transferTest(t, a, b)
	bs, _ := a.Store.Bindings(ctx, office.Session)
	for _, edge := range bs {
		if edge.Status != "accepted" || !edge.VerifiedAt.IsZero() {
			t.Fatal("admission was mistaken for model execution", edge)
		}
	}
	if _, e = a.dispatch(ctx, "/v1/send", APIRequest{Session: office.Session, Channel: "starx-dev", Text: "implicit broadcast"}); e == nil {
		t.Fatal("implicit broadcast accepted")
	}
	// Two independent task IDs/owners for explicit fanout; no shared contested task.
	v, e = a.dispatch(ctx, "/v1/send", APIRequest{Session: office.Session, Channel: "starx-dev", All: true, Kind: "task", Text: "fanout sample"})
	if e != nil {
		t.Fatal(e)
	}
	items := v.(map[string]any)["recipients"].([]map[string]any)
	ids := map[string]bool{}
	for _, item := range items {
		delivery := item["delivery"].(map[string]any)
		ids[delivery["id"].(string)] = true
	}
	if len(ids) != 2 {
		t.Fatal(items)
	}
	transferTest(t, a, b)
	rows, _ := b.Store.List(ctx, 0, 1000, false, home1.Session, "")
	var probe Message
	for _, r := range rows {
		m := r.Message
		if strings.HasPrefix(m.Text, probePrefix) {
			probe = m
		}
		if m.Text == "fanout sample" && m.ToSession != home1.Session {
			t.Fatal("cross-chat delivery")
		}
	}
	if probe.ID == "" {
		t.Fatal("no automatic probe")
	}
	if _, e = b.verifyProbe(ctx, APIRequest{ID: probe.ID, Session: home1.Session}); e == nil {
		t.Fatal("probe completed without model lease")
	}
	if e = b.Store.Claim(ctx, probe.ID, home1.Session, 60); e != nil {
		t.Fatal(e)
	}
	if _, e = b.verifyProbe(ctx, APIRequest{ID: probe.ID, Session: home1.Session}); e == nil {
		t.Fatal("polling model reply was mistaken for native wake")
	}
	if _, e = b.markNativeAdmission(ctx, home1, APIRequest{Messages: []string{probe.ID}, Code: NewID()}); e != nil {
		t.Fatal(e)
	}
	if _, e = b.verifyProbe(ctx, APIRequest{ID: probe.ID, Session: home1.Session}); e != nil {
		t.Fatal(e)
	}
	transferTest(t, b, a)
	bs, _ = a.Store.Bindings(ctx, office.Session)
	verified := 0
	for _, edge := range bs {
		if !edge.VerifiedAt.IsZero() {
			verified++
		}
	}
	if verified != 1 {
		t.Fatal("matching model proof not isolated to exact peer", bs)
	}
	// The other worker on the SAME device cannot wake this chat by spoofing a room.
	inbound := b.base(a.Config.DeviceID, "task")
	inbound.Text = "not the bound worker"
	inbound.Session = "different-worker"
	inbound.ToSession = office.Session
	inbound.Conversation = bindings[0].Channel
	if e = a.acceptIncoming(ctx, inbound, "", nil, "lan"); e != nil {
		t.Fatal(e)
	}
	policy, e := a.wakeEligible(ctx, APIRequest{Session: office.Session, NativeSession: office.NativeSession, ID: inbound.ID})
	if e != nil || policy.(map[string]any)["allowed"].(bool) {
		t.Fatal(policy, e)
	}
	if _, e = a.dispatch(ctx, "/v1/send", APIRequest{Session: office.Session, Channel: "starx-dev", Conversation: "legacy", To: home1.ID, Text: "conflict"}); e == nil {
		t.Fatal("mixed channel/conversation")
	}
	if _, e = a.dispatch(ctx, "/v1/unbind", APIRequest{Session: office.Session, Channel: "starx-dev"}); e != nil {
		t.Fatal(e)
	}
	// A result in flight returns only to the original requester after leaving.
	var task Message
	for id := range ids {
		r, _ := a.Store.Get(ctx, id)
		if r.Message.ToSession == home1.Session {
			task = r.Message
		}
	}
	reply := b.base(a.Config.DeviceID, "result")
	reply.Session = home1.Session
	reply.ToSession = office.Session
	reply.Conversation = task.Conversation
	reply.ReplyTo = task.ID
	reply.Text = "in-flight result"
	if e = a.acceptIncoming(ctx, reply, "", nil, "lan"); e != nil {
		t.Fatal(e)
	}
	policy, e = a.wakeEligible(ctx, APIRequest{Session: office.Session, NativeSession: office.NativeSession, ID: reply.ID})
	if e != nil || !policy.(map[string]any)["allowed"].(bool) {
		t.Fatal("result lost on leave", policy, e)
	}
}
