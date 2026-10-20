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
	key := "binding-probe:" + bindingKey(b)
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
	text := probePrefix + p.Nonce
	result := d.base(m.From, "result")
	result.Session = ep.Session
	result.ToSession = m.Session
	result.Conversation = m.Conversation
	result.ReplyTo = m.ID
	result.Text = text
	if e = d.Store.Complete(ctx, a.ID, ep.Session, text, result); e != nil {
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
	if json.Unmarshal([]byte(d.Store.metadata(ctx, "probe:"+m.ReplyTo)), &p) != nil || m.Text != probePrefix+p.Nonce {
		return nil
	}
	q, e := d.Store.Get(ctx, m.ReplyTo)
	if e != nil {
		return e
	}
	old := q.Message
	if q.Direction != "out" || old.Session != m.ToSession || old.To != m.From || old.ToSession != m.Session || old.Conversation != m.Conversation {
		return nil
	}
	var b Binding
	if json.Unmarshal([]byte(d.Store.metadata(ctx, p.Binding)), &b) != nil || b.Status != "accepted" {
		return nil
	}
	b.VerifiedAt = time.Now().UTC()
	b.VerifiedVersion = p.Version
	return d.Store.saveValue(ctx, bindingKey(b), b)
}
