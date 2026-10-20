package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const controlPrefix = "agent-link/control/v1:"

type sessionControl struct {
	Op        string     `json:"op"`
	Request   string     `json:"request,omitempty"`
	Endpoints []Endpoint `json:"endpoints,omitempty"`
	Source    *Endpoint  `json:"source,omitempty"`
	Channel   string     `json:"channel,omitempty"`
	Name      string     `json:"name,omitempty"`
	TextWake  bool       `json:"text_wake,omitempty"`
}

func (d *Daemon) control(ctx context.Context, to, session, target string, c sessionControl) (Message, error) {
	b, e := json.Marshal(c)
	if e != nil {
		return Message{}, e
	}
	m := d.base(to, "progress")
	m.Session = session
	m.ToSession = target
	m.Text = controlPrefix + string(b)
	m.ReplyTo = c.Request
	if e = d.Store.Enqueue(ctx, m); e != nil {
		return m, e
	}
	d.notify()
	return m, nil
}
func validPublicEndpoint(v Endpoint, device string) bool {
	return v.Device == device && ValidLabel(v.ID) && ValidLabel(v.Session) && ValidLabel(v.Alias) && ValidLabel(v.Harness) && ValidLabel(v.DeviceName) && v.NativeSession == "" && v.Profile == "" && v.StatePath == "" && !v.Local && v.Receiver == "" && v.ReceiverAt.IsZero() && !v.CreatedAt.IsZero()
}
func parseControl(m Message) (sessionControl, error) {
	var c sessionControl
	if m.Kind != "progress" || m.Activity != nil || !strings.HasPrefix(m.Text, controlPrefix) {
		return c, errors.New("not registry metadata")
	}
	dec := json.NewDecoder(strings.NewReader(strings.TrimPrefix(m.Text, controlPrefix)))
	dec.DisallowUnknownFields()
	if e := dec.Decode(&c); e != nil {
		return c, e
	}
	if e := dec.Decode(new(any)); e != io.EOF {
		return c, errors.New("trailing control data")
	}
	if len(c.Endpoints) > 64 {
		return c, errors.New("directory exceeds 64 endpoints")
	}
	if c.Source != nil && !validPublicEndpoint(*c.Source, m.From) {
		return c, errors.New("invalid public source endpoint")
	}
	for _, ep := range c.Endpoints {
		if !validPublicEndpoint(ep, m.From) {
			return c, errors.New("invalid directory endpoint")
		}
	}
	return c, nil
}
func (d *Daemon) receiveControl(ctx context.Context, m Message) error {
	if !strings.HasPrefix(m.Text, controlPrefix) {
		return nil
	}
	c, e := parseControl(m)
	if e != nil {
		return nil
	} // unrecognized metadata remains ordinary stored data
	d.registryMu.Lock()
	defer d.registryMu.Unlock()
	if d.Store.metadata(ctx, "control-done:"+m.ID) != "" {
		return nil
	}
	// Only paired authenticated transports reach this path. A control frame may
	// synchronize addresses or propose scope; it cannot register a local chat.
	switch c.Op {
	case "query":
		if c.Request != "" || c.Source != nil || len(c.Endpoints) != 0 || m.ToSession != "" {
			return nil
		}
		all, e := d.Store.Endpoints(ctx)
		if e != nil {
			return e
		}
		out := []Endpoint{}
		for _, ep := range all {
			if ep.Local {
				out = append(out, publicEndpoint(ep))
				if len(out) == 64 {
					break
				}
			}
		}
		if _, e = d.control(ctx, m.From, "", "", sessionControl{Op: "directory", Request: m.ID, Endpoints: out}); e != nil {
			return e
		}
	case "directory":
		q, e := d.Store.Get(ctx, c.Request)
		if e != nil || q.Direction != "out" || q.Message.To != m.From {
			return nil
		}
		query, e := parseControl(q.Message)
		if e != nil || query.Op != "query" {
			return nil
		}
		for _, ep := range c.Endpoints {
			if ep.Device == d.Config.DeviceID {
				continue
			}
			if e = d.Store.saveValue(ctx, endpointKey(ep.Device, ep.Session), ep); e != nil {
				return e
			}
		}
		if e = d.Store.setMetadata(ctx, "directory-ready:"+m.From, m.ID); e != nil {
			return e
		}
	case "invite":
		if c.Source == nil || c.Source.Session != m.Session || !strings.HasPrefix(c.Channel, "channel-") || !ValidLabel(c.Channel) || !ValidLabel(c.Name) || !ValidLabel(m.ToSession) {
			return nil
		}
		if _, e = d.localEndpoint(ctx, m.ToSession); e != nil {
			return nil
		}
		b := Binding{Channel: c.Channel, ChannelName: c.Name, Session: m.ToSession, Peer: *c.Source, Status: "invited", InviteID: m.ID, TextWake: c.TextWake, UpdatedAt: time.Now().UTC()}
		old := d.Store.metadata(ctx, bindingKey(b))
		var prior Binding
		if old == "" || json.Unmarshal([]byte(old), &prior) == nil && prior.Status == "left" {
			if e = d.Store.saveValue(ctx, bindingKey(b), b); e != nil {
				return e
			}
		}
	case "accepted":
		if c.Source == nil || c.Source.Session != m.Session {
			return nil
		}
		bs, e := d.Store.Bindings(ctx, m.ToSession)
		if e != nil {
			return e
		}
		for _, b := range bs {
			if b.InviteID == c.Request && b.Status == "proposed" && b.Channel == c.Channel && b.Peer.Device == m.From && b.Peer.Session == m.Session {
				b.Status = "accepted"
				b.Peer = *c.Source
				b.UpdatedAt = time.Now().UTC()
				if e = d.Store.saveValue(ctx, bindingKey(b), b); e != nil {
					return e
				}
				if e = d.probeBinding(ctx, b); e != nil {
					return e
				}
			}
		}
	case "leave":
		bs, e := d.Store.Bindings(ctx, m.ToSession)
		if e != nil {
			return e
		}
		for _, b := range bs {
			if b.Channel == c.Channel && b.Peer.Device == m.From && b.Peer.Session == m.Session {
				b.Status = "left"
				b.UpdatedAt = time.Now().UTC()
				if e = d.Store.saveValue(ctx, bindingKey(b), b); e != nil {
					return e
				}
			}
		}
	default:
		return nil
	}
	return d.Store.setMetadata(ctx, "control-done:"+m.ID, "1")
}
func (d *Daemon) sessionDispatch(ctx context.Context, path string, a APIRequest) (any, error) {
	d.registryMu.Lock()
	defer d.registryMu.Unlock()
	switch path {
	case "/v1/session/verify":
		return d.verifyProbe(ctx, a)
	case "/v1/session/attach":
		return d.attach(ctx, a)
	case "/v1/session/list":
		if a.To != "" {
			to, e := d.Transport.Resolve(ctx, a.To)
			if e != nil {
				return nil, e
			}
			if to != d.Config.DeviceID {
				p, e := d.Store.Pair(ctx, to)
				if e != nil || !p.Ready() {
					return nil, errors.New("session discovery requires an already paired device")
				}
				m, e := d.control(ctx, to, "", "", sessionControl{Op: "query"})
				if e != nil {
					return nil, e
				}
				return map[string]any{"status": "directory_requested", "id": m.ID, "next": "run session list again after delivery; no reply means unsupported or offline"}, nil
			}
		}
		return d.Store.Endpoints(ctx)
	case "/v1/session/current":
		if a.NativeSession != "" {
			all, e := d.Store.Endpoints(ctx)
			if e != nil {
				return nil, e
			}
			for _, v := range all {
				if v.Local && v.NativeSession == a.NativeSession && v.Harness == a.Harness && v.Profile == a.Profile {
					return v, nil
				}
			}
			return nil, errors.New("current native chat is not registered")
		}
		return d.localEndpoint(ctx, a.Session)
	case "/v1/session/pause", "/v1/session/resume", "/v1/session/receiver":
		ep, e := d.localEndpoint(ctx, a.Session)
		if e != nil {
			return nil, e
		}
		if path == "/v1/session/receiver" {
			if a.NativeSession != ep.NativeSession {
				return nil, errors.New("native identity mismatch")
			}
			if a.State == "admitted" {
				return d.markNativeAdmission(ctx, ep, a)
			}
			if a.State != "connected" && a.State != "unavailable" {
				return nil, errors.New("invalid receiver state")
			}
			ep.Receiver = a.State
			ep.ReceiverAt = time.Now().UTC()
			if a.State == "connected" {
				bs, e := d.Store.Bindings(ctx, ep.Session)
				if e != nil {
					return nil, e
				}
				for _, b := range bs {
					if b.Status == "accepted" {
						if e = d.probeBinding(ctx, b); e != nil {
							return nil, e
						}
					}
				}
			}
		} else {
			ep.Paused = path == "/v1/session/pause"
		}
		e = d.Store.saveValue(ctx, endpointKey(ep.Device, ep.Session), ep)
		d.notify()
		return ep, e
	case "/v1/wake/eligible":
		return d.wakeEligible(ctx, a)
	case "/v1/bindings":
		return d.Store.Bindings(ctx, a.Session)
	case "/v1/bind":
		ep, e := d.localEndpoint(ctx, a.Session)
		if e != nil {
			return nil, e
		}
		if a.ID != "" { // Accept precisely one locally received invitation in this chat.
			bs, e := d.Store.Bindings(ctx, a.Session)
			if e != nil {
				return nil, e
			}
			for _, b := range bs {
				if b.InviteID == a.ID && (b.Status == "invited" || b.Status == "accepted") {
					p := publicEndpoint(ep)
					if _, e = d.control(ctx, b.Peer.Device, ep.Session, b.Peer.Session, sessionControl{Op: "accepted", Request: b.InviteID, Source: &p, Channel: b.Channel}); e != nil {
						return nil, e
					}
					b.Status = "accepted"
					b.TextWake = a.TextWake
					b.UpdatedAt = time.Now().UTC()
					if e = d.Store.saveValue(ctx, bindingKey(b), b); e != nil {
						return nil, e
					}
					if e = d.probeBinding(ctx, b); e != nil {
						return nil, e
					}
					return b, nil
				}
			}
			return nil, errors.New("no invitation for this exact local chat; inspect bindings")
		}
		if !ValidLabel(a.Channel) || len(a.Targets) == 0 || len(a.Targets) > 32 {
			return nil, errors.New("bind needs --channel and 1–32 exact targets, or --accept invitation ID")
		}
		channel, e := d.channel(ctx, a.Session, a.Channel)
		if e != nil {
			if strings.Contains(e.Error(), "ambiguous") {
				return nil, e
			}
			channel = channelID(ep.Session, a.Channel)
		}
		all, e := d.Store.Endpoints(ctx)
		if e != nil {
			return nil, e
		}
		out := []Binding{}
		for _, name := range a.Targets {
			peer, e := resolveEndpoint(all, name)
			if e != nil {
				return nil, e
			}
			if peer.Device == ep.Device && peer.Session == ep.Session {
				return nil, errors.New("cannot bind a chat to itself")
			}
			if peer.Device != ep.Device {
				p, e := d.Store.Pair(ctx, peer.Device)
				if e != nil || !p.Ready() {
					return nil, errors.New("peer device is not paired")
				}
			}
			b := Binding{Channel: channel, ChannelName: a.Channel, Session: ep.Session, Peer: publicEndpoint(peer), Status: "proposed", TextWake: a.TextWake, UpdatedAt: time.Now().UTC()}
			if old := d.Store.metadata(ctx, bindingKey(b)); old != "" {
				var prior Binding
				if json.Unmarshal([]byte(old), &prior) == nil && (prior.Status == "accepted" || prior.Status == "proposed") {
					out = append(out, prior)
					continue
				}
			}
			source := publicEndpoint(ep)
			m, e := d.control(ctx, peer.Device, ep.Session, peer.Session, sessionControl{Op: "invite", Channel: channel, Name: a.Channel, Source: &source, TextWake: a.TextWake})
			if e != nil {
				return nil, e
			}
			b.InviteID = m.ID
			if e = d.Store.saveValue(ctx, bindingKey(b), b); e != nil {
				return nil, e
			}
			out = append(out, b)
		}
		return out, nil
	case "/v1/unbind":
		channel, e := d.channel(ctx, a.Session, a.Channel)
		if e != nil {
			return nil, e
		}
		bs, e := d.Store.Bindings(ctx, a.Session)
		if e != nil {
			return nil, e
		}
		for _, b := range bs {
			if b.Channel == channel && b.Status != "left" {
				b.Status = "left"
				b.UpdatedAt = time.Now().UTC()
				if _, e = d.control(ctx, b.Peer.Device, a.Session, b.Peer.Session, sessionControl{Op: "leave", Channel: channel}); e != nil {
					return nil, e
				}
				if e = d.Store.saveValue(ctx, bindingKey(b), b); e != nil {
					return nil, e
				}
			}
		}
		d.notify()
		return map[string]string{"status": "left", "channel": channel}, nil
	}
	return nil, fmt.Errorf("unknown session operation %s", path)
}
