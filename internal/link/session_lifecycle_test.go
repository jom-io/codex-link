package link

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestArchiveReleasesOnlyItsIdleInboxWait(t *testing.T) {
	d, _, ep := lifecycleFixture(t)
	ctx := context.Background()
	other := attachTest(t, d, "wait-other-native")
	presenceTest(t, d, ep, "idle")
	startWait := func(session string) (<-chan error, context.CancelFunc) {
		waitCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() {
			v, e := d.dispatch(waitCtx, "/v1/wait", APIRequest{Session: session, Timeout: 30, Limit: 100})
			if e == nil && len(v.([]Record)) != 0 {
				e = errors.New("archive notification fabricated a message")
			}
			done <- e
		}()
		return done, cancel
	}
	archivedWait, cancelArchived := startWait(ep.Session)
	defer cancelArchived()
	otherWait, cancelOther := startWait(other.Session)
	defer cancelOther()
	legacyWait, cancelLegacy := startWait("unregistered-legacy-worker")
	defer cancelLegacy()
	if _, e := d.dispatch(ctx, "/v1/session/detach", APIRequest{Session: ep.Session}); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-archivedWait:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("archived chat kept its idle watcher waiting for an unrelated message")
	}
	for _, done := range []<-chan error{otherWait, legacyWait} {
		select {
		case e := <-done:
			t.Fatal("another chat or unregistered legacy wait was interrupted", e)
		case <-time.After(25 * time.Millisecond):
		}
	}
	// The same result must hold when the caller starts after archival, and it
	// must not require the native adapter's minute-long presence timer.
	bounded, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if v, e := d.dispatch(bounded, "/v1/wait", APIRequest{Session: ep.Session, Timeout: 30, Limit: 100}); e != nil || len(v.([]Record)) != 0 {
		t.Fatal("already archived wait did not return empty promptly", v, e)
	}
	cancelOther()
	cancelLegacy()
	for _, done := range []<-chan error{otherWait, legacyWait} {
		if e := <-done; !errors.Is(e, context.Canceled) {
			t.Fatal("unrelated wait changed its cancellation semantics", e)
		}
	}
	// An archived reader can still retrieve saved in-flight replies. The early
	// empty return is a lifecycle signal, never a reason to hide inbox history.
	m := sample("result")
	m.To, m.ToSession = d.Config.DeviceID, ep.Session
	if e := d.Store.Accept(ctx, &m, "", nil); e != nil {
		t.Fatal(e)
	}
	v, e := d.dispatch(ctx, "/v1/wait", APIRequest{Session: ep.Session, Timeout: 30, Limit: 100})
	if e != nil || len(v.([]Record)) != 1 || v.([]Record)[0].Message.ID != m.ID {
		t.Fatal("archived wait hid saved history", v, e)
	}
}

func lifecycleFixture(t *testing.T) (*Daemon, *time.Time, Endpoint) {
	t.Helper()
	d, _ := pairedRoutingDaemons(t, "127.0.0.1:1")
	now := time.Now().UTC()
	d.lifecycleNow = func() time.Time { return now }
	ep := attachTest(t, d, "lifecycle-native")
	return d, &now, ep
}
func presenceTest(t *testing.T, d *Daemon, ep Endpoint, state string) Endpoint {
	t.Helper()
	v, e := d.dispatch(context.Background(), "/v1/session/presence", APIRequest{Session: ep.Session, NativeSession: ep.NativeSession, State: state})
	if e != nil {
		t.Fatal(e)
	}
	return v.(Endpoint)
}
func gcTest(t *testing.T, d *Daemon, ep Endpoint, apply bool) []GCDecision {
	t.Helper()
	v, e := d.dispatch(context.Background(), "/v1/session/gc", APIRequest{Session: ep.Session, Apply: apply})
	if e != nil {
		t.Fatal(e)
	}
	return v.([]GCDecision)
}
func TestLifecycleIdleLeaseAndProtectedNativeStates(t *testing.T) {
	d, now, ep := lifecycleFixture(t)
	ctx := context.Background()
	initial := ep.LastUsedAt
	for _, state := range []string{"idle", "running", "waiting_user", "uncertain"} {
		*now = now.Add(8 * 24 * time.Hour)
		fresh := presenceTest(t, d, ep, state)
		if fresh.Status != "online" || !fresh.LastUsedAt.Equal(initial) {
			t.Fatal("passive presence changed business use or lost idle liveness", fresh)
		}
		if got := gcTest(t, d, ep, true); len(got) != 0 {
			t.Fatal("live idle/active/approval chat became a candidate", got)
		}
	}
	*now = now.Add(8 * 24 * time.Hour)
	presenceTest(t, d, ep, "unknown")
	got := gcTest(t, d, ep, true)
	if len(got) != 1 || got[0].Archived || !strings.Contains(strings.Join(got[0].Blocked, ","), "native-uncertain") {
		t.Fatal("unknown erased native safety protection", got)
	}
	if _, e := d.dispatch(ctx, "/v1/session/presence", APIRequest{Session: ep.Session, NativeSession: "other-chat", State: "idle"}); e == nil {
		t.Fatal("wrong native renewed presence")
	}
	all, e := d.dispatch(ctx, "/v1/session/list", APIRequest{View: "active"})
	if e != nil || len(all.([]Endpoint)) != 0 {
		t.Fatal("expired lease still shown as online", all, e)
	}
}
func TestLifecyclePreviewArchiveRestorePreservesIdentityBindingsAndPause(t *testing.T) {
	d, now, ep := lifecycleFixture(t)
	ctx := context.Background()
	other := attachTest(t, d, "other-native")
	binding := Binding{Session: ep.Session, Channel: "channel-lifecycle", ChannelName: "lifecycle", Status: "accepted", Peer: publicEndpoint(other), UpdatedAt: *now}
	if e := d.Store.saveValue(ctx, bindingKey(binding), binding); e != nil {
		t.Fatal(e)
	}
	if _, e := d.dispatch(ctx, "/v1/session/pause", APIRequest{Session: ep.Session}); e != nil {
		t.Fatal(e)
	}
	presenceTest(t, d, ep, "idle")
	presenceTest(t, d, ep, "missing")
	*now = now.Add(8 * 24 * time.Hour)
	preview := gcTest(t, d, ep, false)
	if len(preview) != 1 || preview[0].Archived || len(preview[0].Blocked) != 0 {
		t.Fatal(preview)
	}
	before, _ := d.Store.Bindings(ctx, ep.Session)
	archived := gcTest(t, d, ep, true)
	if len(archived) != 1 || !archived[0].Archived {
		t.Fatal(archived)
	}
	policy, e := d.wakeEligible(ctx, APIRequest{Session: ep.Session, NativeSession: ep.NativeSession})
	if e != nil || !policy.(map[string]any)["archived"].(bool) {
		t.Fatal(policy, e)
	}
	if got := presenceTest(t, d, ep, "idle"); got.Status != "archived" {
		t.Fatal("passive observation restored an archived chat", got)
	}
	if _, e = d.dispatch(ctx, "/v1/send", APIRequest{To: d.Config.DeviceID, ToSession: ep.Session, Session: other.Session, Kind: "task", Text: "do not enqueue"}); e == nil {
		t.Fatal("new task to archived target was accepted")
	}
	if e = d.Store.Close(); e != nil {
		t.Fatal(e)
	}
	d.Store, e = OpenStore(d.Home)
	if e != nil {
		t.Fatal(e)
	}
	restored := attachTest(t, d, ep.NativeSession)
	after, _ := d.Store.Bindings(ctx, ep.Session)
	if restored.ID != ep.ID || restored.Session != ep.Session || restored.NativeSession != ep.NativeSession || restored.Alias != ep.Alias || !restored.Paused || !restored.Wake || !restored.ArchivedAt.IsZero() || !reflect.DeepEqual(before, after) {
		t.Fatal("restore changed identity/scope/pause", ep, restored, before, after)
	}
	untouched, _ := d.Store.Endpoint(ctx, other.Device, other.Session)
	if !untouched.ArchivedAt.IsZero() {
		t.Fatal("another chat affected")
	}
	if _, e = d.dispatch(ctx, "/v1/session/attach", APIRequest{Session: ep.Session, NativeSession: "different-native", Profile: "default", Harness: ep.Harness}); e == nil {
		t.Fatal("another chat reused archived identity")
	}
}
func TestLifecycleTemporaryPinGraceAndWorkGuards(t *testing.T) {
	for _, kind := range []string{"none", "task", "result", "file", "out-task", "out-file", "lease"} {
		t.Run(kind, func(t *testing.T) {
			d, now, ep := lifecycleFixture(t)
			ctx := context.Background()
			*now = now.Add(26 * time.Hour)
			if kind != "none" {
				m := d.base(d.Config.DeviceID, kind)
				m.ToSession = ep.Session
				m.Session = "peer-worker"
				m.From = "peer-device"
				m.Kind = kind
				m.Text = "non-secret work guard sample"
				if strings.HasPrefix(kind, "out-") {
					m.Kind = strings.TrimPrefix(kind, "out-")
					if m.Kind == "file" {
						m.File = &FileRef{Name: "sample.txt", Size: 1, SHA256: strings.Repeat("a", 64), Object: "codex-link/sample"}
					}
					m.From = d.Config.DeviceID
					m.Session = ep.Session
					m.To = "peer-device"
					m.ToSession = "peer-worker"
					if e := d.Store.Enqueue(ctx, m); e != nil {
						t.Fatal(e)
					}
				} else {
					if kind == "lease" {
						m.Kind = "task"
						m.ToSession = ""
					}
					if e := d.Store.Accept(ctx, &m, "", nil); e != nil {
						t.Fatal(e)
					}
					if kind == "lease" {
						if e := d.Store.Claim(ctx, m.ID, ep.Session, 60); e != nil {
							t.Fatal(e)
						}
					}
				}
			}
			got := gcTest(t, d, ep, false)
			if len(got) != 1 {
				t.Fatal(got)
			}
			if kind != "none" {
				if len(got[0].Blocked) == 0 || gcTest(t, d, ep, true)[0].Archived {
					t.Fatal("pending work archived", kind, got)
				}
				return
			}
			if _, e := d.dispatch(ctx, "/v1/session/pin", APIRequest{Session: ep.Session}); e != nil {
				t.Fatal(e)
			}
			if gcTest(t, d, ep, true)[0].Archived {
				t.Fatal("pinned archived")
			}
			if _, e := d.dispatch(ctx, "/v1/session/unpin", APIRequest{Session: ep.Session}); e != nil {
				t.Fatal(e)
			}
			d.lifecycleStarted = *now
			if gcTest(t, d, ep, true)[0].Archived {
				t.Fatal("restart grace applied too late")
			}
			*now = now.Add(6 * time.Minute)
			if !gcTest(t, d, ep, true)[0].Archived {
				t.Fatal("unused temporary not archived")
			}
		})
	}
}
func TestLifecycleAcknowledgedResultsDoNotBlockButReadsAndPollingDo(t *testing.T) {
	d, now, ep := lifecycleFixture(t)
	ctx := context.Background()
	m := d.base(d.Config.DeviceID, "result")
	m.From = "peer-device"
	m.Session = "peer-worker"
	m.ToSession = ep.Session
	m.Text = "non-secret lifecycle sample"
	if e := d.Store.Accept(ctx, &m, "", nil); e != nil {
		t.Fatal(e)
	}
	before, _ := d.Store.Get(ctx, m.ID)
	*now = now.Add(26 * time.Hour)
	if _, e := d.dispatch(ctx, "/v1/inbox", APIRequest{Session: ep.Session, Limit: 100}); e != nil {
		t.Fatal(e)
	}
	got := gcTest(t, d, ep, true)
	if len(got) != 1 || got[0].Archived {
		t.Fatal("reading consumed result or reset last-use", got)
	}
	if _, e := d.dispatch(ctx, "/v1/inbox/ack", APIRequest{Session: ep.Session, Messages: []string{m.ID}}); e != nil {
		t.Fatal(e)
	}
	if got = gcTest(t, d, ep, false); len(got) != 0 {
		t.Fatal("real handling did not refresh use", got)
	}
	*now = now.Add(26 * time.Hour)
	if !gcTest(t, d, ep, true)[0].Archived {
		t.Fatal("handled result blocked archival")
	}
	after, _ := d.Store.Get(ctx, m.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("history changed")
	}
}
func TestLifecycleMigrationAndManualFreshness(t *testing.T) {
	d, now, ep := lifecycleFixture(t)
	ctx := context.Background()
	ep.LifecycleVersion = 0
	ep.LifecycleSince = time.Time{}
	ep.LastUsedAt = time.Time{}
	ep.UpdatedAt = now.Add(-100 * 24 * time.Hour)
	if e := d.Store.saveValue(ctx, endpointKey(ep.Device, ep.Session), ep); e != nil {
		t.Fatal(e)
	}
	if e := d.initLifecycle(ctx); e != nil {
		t.Fatal(e)
	}
	stored, _ := d.Store.Endpoint(ctx, ep.Device, ep.Session)
	if !stored.LastUsedAt.Equal(*now) || !stored.LastHostSeenAt.IsZero() {
		t.Fatal("migration fabricated presence or trusted old timestamp", stored)
	}
	if _, e := d.dispatch(ctx, "/v1/session/detach", APIRequest{Session: ep.Session}); e == nil {
		t.Fatal("unobserved detach succeeded")
	}
	presenceTest(t, d, ep, "running")
	if _, e := d.dispatch(ctx, "/v1/session/detach", APIRequest{Session: ep.Session}); e == nil {
		t.Fatal("active chat detached")
	}
	presenceTest(t, d, ep, "idle")
	*now = now.Add(6 * time.Second)
	if _, e := d.dispatch(ctx, "/v1/session/detach", APIRequest{Session: ep.Session}); e == nil {
		t.Fatal("stale observation detached")
	}
	presenceTest(t, d, ep, "idle")
	if _, e := d.dispatch(ctx, "/v1/session/detach", APIRequest{Session: ep.Session}); e != nil {
		t.Fatal(e)
	}
}
func TestLifecycleConfigValidation(t *testing.T) {
	p := SessionPolicy{}
	if e := p.validate(); e != nil {
		t.Fatal(e)
	}
	p.LeaseSeconds = 1
	if e := p.validate(); e == nil {
		t.Fatal("unsafe tiny lease accepted")
	}
	_, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
}

func TestLifecycleOldPublicTaskReplyIsRecognizedWithoutWeakeningDirectedPeer(t *testing.T) {
	for _, directed := range []bool{false, true} {
		t.Run(map[bool]string{false: "public", true: "directed"}[directed], func(t *testing.T) {
			d, now, ep := lifecycleFixture(t)
			ctx := context.Background()
			m := d.base("peer-device", "task")
			m.Session = ep.Session
			m.Text = "old non-secret public task"
			if directed {
				m.ToSession = "assigned-worker"
			}
			if e := d.Store.Enqueue(ctx, m); e != nil {
				t.Fatal(e)
			}
			if e := d.Store.MarkSent(ctx, m.ID); e != nil {
				t.Fatal(e)
			}
			if _, e := d.Store.db.ExecContext(ctx, "UPDATE messages SET status='delivered' WHERE id=?", m.ID); e != nil {
				t.Fatal(e)
			}
			result := d.base(d.Config.DeviceID, "result")
			result.From = "peer-device"
			result.Session = "different-actual-worker"
			result.ToSession = ep.Session
			result.ReplyTo = m.ID
			result.Text = "verified scope reply"
			if e := d.Store.Accept(ctx, &result, "", nil); e != nil {
				t.Fatal(e)
			}
			if _, e := d.Store.AckInbox(ctx, d.Config.DeviceID, ep.Session, []string{result.ID}); e != nil {
				t.Fatal(e)
			}
			*now = now.Add(26 * time.Hour)
			got := gcTest(t, d, ep, true)
			if len(got) != 1 || got[0].Archived == directed {
				t.Fatal("public reply ignored or directed worker weakened", directed, got)
			}
		})
	}
}

func TestLifecycleUncertainJournalBlocksEvenWithIdleObservation(t *testing.T) {
	d, now, ep := lifecycleFixture(t)
	ctx := context.Background()
	ep.Harness = "codex"
	ep.StatePath = filepath.Join(t.TempDir(), "wake.json")
	if e := d.Store.saveValue(ctx, endpointKey(ep.Device, ep.Session), ep); e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(map[string]any{"session": ep.Session, "nativeSessionId": ep.NativeSession, "entries": []map[string]string{{"id": NewID(), "status": "intent"}}})
	if e := os.WriteFile(ep.StatePath+".codex-submissions.json", b, 0600); e != nil {
		t.Fatal(e)
	}
	presenceTest(t, d, ep, "idle")
	if _, e := d.dispatch(ctx, "/v1/session/detach", APIRequest{Session: ep.Session}); e == nil {
		t.Fatal("idle native observation bypassed uncertain admission journal")
	}
	presenceTest(t, d, ep, "missing")
	*now = now.Add(8 * 24 * time.Hour)
	got := gcTest(t, d, ep, true)
	if len(got) != 1 || got[0].Archived || !strings.Contains(strings.Join(got[0].Blocked, ","), "uncertain-admission-journal") {
		t.Fatal(got)
	}
}

func TestLifecycleEstablishedLegacyReceiverIsNotAnUnusedTemporary(t *testing.T) {
	d, now, ep := lifecycleFixture(t)
	ctx := context.Background()
	ep.LifecycleVersion = 0
	ep.Receiver = "connected"
	ep.ReceiverAt = *now
	ep.LifecycleSince = time.Time{}
	ep.LastUsedAt = time.Time{}
	if e := d.Store.saveValue(ctx, endpointKey(ep.Device, ep.Session), ep); e != nil {
		t.Fatal(e)
	}
	if e := d.initLifecycle(ctx); e != nil {
		t.Fatal(e)
	}
	*now = now.Add(26 * time.Hour)
	if got := gcTest(t, d, ep, true); len(got) != 0 {
		t.Fatal("established legacy receiver treated as unused temporary", got)
	}
	*now = now.Add(8 * 24 * time.Hour)
	got := gcTest(t, d, ep, true)
	if len(got) != 1 || got[0].Archived || !strings.Contains(strings.Join(got[0].Blocked, ","), "native-state-unconfirmed") {
		t.Fatal("unconfirmed established legacy receiver archived", got)
	}
	presenceTest(t, d, ep, "running")
	if got = gcTest(t, d, ep, true); len(got) != 0 {
		t.Fatal("fresh active lease lost", got)
	}
}
