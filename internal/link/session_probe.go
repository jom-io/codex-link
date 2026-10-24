package link

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const probePrefix = "agent-link/probe/v1:"

type bindingProbe struct {
	Nonce   string `json:"nonce"`
	Channel string `json:"channel"`
	Binding string `json:"binding"`
	Version string `json:"version"`
}

func (d *Daemon) probeBinding(ctx context.Context, b Binding) error {
	key := "binding-native-probe:" + bindingKey(b)
	if d.Store.metadata(ctx, key) == Version {
		return nil
	}
	m := d.base(b.Peer.Device, "task")
	m.Session = b.Session
	m.ToSession = b.Peer.Session
	m.Conversation = b.Channel
	p := bindingProbe{Nonce: NewID(), Channel: b.Channel, Binding: bindingKey(b), Version: Version}
	body, _ := json.Marshal(p)
	m.Text = probePrefix + string(body) + "\nNon-secret session binding wake verification. In this exact chat, claim this task's own ID with the retained worker, then run agent-link session verify --probe " + m.ID + " --session " + b.Peer.Session + ". This only verifies this binding; it grants no other execution/file permission. Do not forward to another chat."
	if e := d.Store.saveValue(ctx, "probe:"+m.ID, p); e != nil {
		return e
	}
	if e := d.Store.Enqueue(ctx, m); e != nil {
		return e
	}
	if b.VerifiedEvent == "" {
		b.VerifiedAt = time.Time{}
		b.VerifiedVersion = ""
	}
	b.ProbeID = m.ID
	b.ProbeVersion = Version
	if e := d.Store.saveValue(ctx, bindingKey(b), b); e != nil {
		return e
	}
	if e := d.Store.setMetadata(ctx, key, Version); e != nil {
		return e
	}
	d.notify()
	return nil
}
func (d *Daemon) verifyProbe(ctx context.Context, a APIRequest) (any, error) {
	ep, e := d.localEndpoint(ctx, a.Session)
	if e != nil {
		return nil, e
	}
	r, e := d.Store.Get(ctx, a.ID)
	if e != nil {
		return nil, e
	}
	m := r.Message
	if r.Direction != "in" || m.Kind != "task" || m.ToSession != ep.Session || r.Owner != ep.Session || r.Status != "claimed" || r.LeaseUntil <= time.Now().Unix() || !strings.HasPrefix(m.Text, probePrefix) {
		return nil, errors.New("verification requires this exact chat's active lease on a binding probe")
	}
	var p bindingProbe
	if json.Unmarshal([]byte(strings.SplitN(strings.TrimPrefix(m.Text, probePrefix), "\n", 2)[0]), &p) != nil || !ValidLabel(p.Nonce) || p.Channel != m.Conversation {
		return nil, errors.New("invalid binding probe")
	}
	policy, e := d.wakeEligible(ctx, APIRequest{Session: ep.Session, NativeSession: ep.NativeSession, ID: a.ID})
	if e != nil {
		return nil, e
	}
	if !policy.(map[string]any)["allowed"].(bool) {
		return nil, errors.New("probe source is not accepted in this channel")
	}
	var admission nativeAdmission
	if json.Unmarshal([]byte(d.Store.metadata(ctx, "native-admission:"+m.ID)), &admission) != nil || admission.Session != ep.Session || admission.Native != ep.NativeSession || !ValidLabel(admission.Event) {
		return nil, errors.New("probe has no native notification admission for this chat; wait for the queued idle wake, do not verify from the current polling turn")
	}
	proof, _ := json.Marshal(probeReply{Nonce: p.Nonce, Event: admission.Event})
	text := probePrefix + string(proof)
	result := d.base(m.From, "result")
	result.Session = ep.Session
	result.ToSession = m.Session
	result.Conversation = m.Conversation
	result.ReplyTo = m.ID
	result.Text = text
	if e = d.Store.Complete(ctx, a.ID, ep.Session, text, result); e != nil {
		return nil, e
	}
	r.LeaseUntil = 0
	if e = d.reportActivity(ctx, r, ep.Session, "completed", ""); e != nil {
		return nil, e
	}
	d.notify()
	return map[string]string{"status": "completed", "reply_id": result.ID}, nil
}
func (d *Daemon) receiveProbeResult(ctx context.Context, m Message) error {
	d.registryMu.Lock()
	defer d.registryMu.Unlock()
	if m.Kind != "result" || !strings.HasPrefix(m.Text, probePrefix) {
		return nil
	}
	var p bindingProbe
	var reply probeReply
	if json.Unmarshal([]byte(d.Store.metadata(ctx, "probe:"+m.ReplyTo)), &p) != nil || json.Unmarshal([]byte(strings.TrimPrefix(m.Text, probePrefix)), &reply) != nil || reply.Nonce != p.Nonce || !ValidLabel(reply.Event) || len(reply.Event) != 32 {
		return nil
	}
	q, e := d.Store.Get(ctx, m.ReplyTo)
	if e != nil {
		return e
	}
	old := q.Message
	if !d.sentRecord(q) || old.Session != m.ToSession || old.To != m.From || old.ToSession != m.Session || old.Conversation != m.Conversation {
		return nil
	}
	var b Binding
	if json.Unmarshal([]byte(d.Store.metadata(ctx, p.Binding)), &b) != nil || b.Status != "accepted" {
		return nil
	}
	b.VerifiedAt = time.Now().UTC()
	b.VerifiedVersion = p.Version
	b.VerifiedEvent = reply.Event
	return d.Store.saveValue(ctx, bindingKey(b), b)
}

type nativeAdmission struct {
	Session    string    `json:"session"`
	Native     string    `json:"native"`
	Event      string    `json:"event"`
	AcceptedAt time.Time `json:"accepted_at"`
}
type probeReply struct {
	Nonce string `json:"nonce"`
	Event string `json:"event"`
}

func (d *Daemon) markNativeAdmission(ctx context.Context, ep Endpoint, a APIRequest) (any, error) {
	if !ep.ArchivedAt.IsZero() {
		return nil, errors.New("archived chat cannot record new native admission")
	}
	if len(a.Messages) < 1 || len(a.Messages) > 20 || !ValidLabel(a.Code) || len(a.Code) != 32 {
		return nil, errors.New("invalid native admission metadata")
	}
	for _, id := range a.Messages {
		r, e := d.Store.Get(ctx, id)
		if e != nil {
			return nil, e
		}
		if r.Direction != "in" || r.Message.ToSession != ep.Session {
			return nil, errors.New("native admission targets another chat")
		}
	}
	for _, id := range a.Messages {
		v := nativeAdmission{Session: ep.Session, Native: ep.NativeSession, Event: a.Code, AcceptedAt: time.Now().UTC()}
		if e := d.Store.saveValue(ctx, "native-admission:"+id, v); e != nil {
			return nil, e
		}
	}
	ep.LastUsedAt = d.sessionNow()
	if e := d.Store.saveValue(ctx, endpointKey(ep.Device, ep.Session), ep); e != nil {
		return nil, e
	}
	return map[string]string{"status": "native_admission_recorded"}, nil
}
