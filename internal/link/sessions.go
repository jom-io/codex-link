package link

// Persistent chat addresses. Native chat identifiers never leave this machine.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type Endpoint struct {
	ID            string    `json:"id"`
	Device        string    `json:"device"`
	DeviceName    string    `json:"device_name"`
	Session       string    `json:"session"`
	Alias         string    `json:"alias"`
	Harness       string    `json:"harness"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Local         bool      `json:"local"`
	Profile       string    `json:"profile,omitempty"`
	NativeSession string    `json:"native_session,omitempty"`
	Wake          bool      `json:"wake"`
	Paused        bool      `json:"paused"`
	Receiver      string    `json:"receiver,omitempty"`
	ReceiverAt    time.Time `json:"receiver_at,omitempty"`
	VerifiedAt    time.Time `json:"verified_at,omitempty"`
	StatePath     string    `json:"state_path,omitempty"`
}

type Binding struct {
	ProbeID         string    `json:"probe_id,omitempty"`
	ProbeVersion    string    `json:"probe_version,omitempty"`
	VerifiedEvent   string    `json:"verified_event,omitempty"`
	VerifiedVersion string    `json:"verified_version,omitempty"`
	Channel         string    `json:"channel"`
	ChannelName     string    `json:"channel_name"`
	Session         string    `json:"session"`
	Peer            Endpoint  `json:"peer"`
	Status          string    `json:"status"`
	InviteID        string    `json:"invite_id"`
	TextWake        bool      `json:"text_wake"`
	UpdatedAt       time.Time `json:"updated_at"`
	VerifiedAt      time.Time `json:"verified_at,omitempty"`
}

func endpointKey(device, session string) string { return "endpoint:" + device + ":" + session }
func bindingKey(b Binding) string {
	return "binding:" + b.Session + ":" + b.Channel + ":" + b.Peer.Device + ":" + b.Peer.Session
}
func (s *Store) saveValue(ctx context.Context, key string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return s.setMetadata(ctx, key, string(b))
}
func (s *Store) values(ctx context.Context, prefix string, dst func([]byte) error) error {
	rows, e := s.db.QueryContext(ctx, "SELECT value FROM metadata WHERE key LIKE ? ORDER BY key", prefix+"%")
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		if e = rows.Scan(&b); e != nil {
			return e
		}
		if e = dst([]byte(b)); e != nil {
			return e
		}
	}
	return rows.Err()
}
func (s *Store) Endpoints(ctx context.Context) ([]Endpoint, error) {
	out := []Endpoint{}
	e := s.values(ctx, "endpoint:", func(b []byte) error {
		var v Endpoint
		if e := json.Unmarshal(b, &v); e != nil {
			return e
		}
		out = append(out, v)
		return nil
	})
	return out, e
}
func (s *Store) Bindings(ctx context.Context, session string) ([]Binding, error) {
	out := []Binding{}
	e := s.values(ctx, "binding:", func(b []byte) error {
		var v Binding
		if e := json.Unmarshal(b, &v); e != nil {
			return e
		}
		if session == "" || v.Session == session {
			out = append(out, v)
		}
		return nil
	})
	return out, e
}
func (s *Store) Endpoint(ctx context.Context, device, session string) (Endpoint, error) {
	var v Endpoint
	b := s.metadata(ctx, endpointKey(device, session))
	if b == "" {
		return v, errors.New("chat is not registered; run session attach in that chat")
	}
	e := json.Unmarshal([]byte(b), &v)
	return v, e
}
func publicEndpoint(v Endpoint) Endpoint {
	v.Local = false
	v.NativeSession = ""
	v.Profile = ""
	v.StatePath = ""
	v.Receiver = ""
	v.ReceiverAt = time.Time{}
	return v
}
func defaultAlias(device, harness, id string, now time.Time) string {
	device = strings.TrimSuffix(device, "-mac")
	switch harness {
	case "deepseek-harness":
		harness = "ds"
	case "claude-code":
		harness = "claude"
	}
	base := device + "-" + harness
	if len(base) > 40 {
		base = base[:40]
	}
	return base + "-" + now.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("0102-1504") + "-" + id[:4]
}
func (d *Daemon) attach(ctx context.Context, a APIRequest) (Endpoint, error) {
	if a.Path != "" && !filepath.IsAbs(a.Path) {
		return Endpoint{}, errors.New("wake state path must be absolute")
	}
	if !ValidLabel(a.Harness) || !ValidLabel(a.Profile) || a.NativeSession == "" || len(a.NativeSession) > 256 || strings.ContainsAny(a.NativeSession, "\x00\r\n") {
		return Endpoint{}, errors.New("attach requires this chat's native context, harness and profile; never guess another chat")
	}
	if a.Name != "" && !ValidLabel(a.Name) {
		return Endpoint{}, errors.New("name must be 1–64 ASCII letters, digits, '.', '_' or '-'")
	}
	all, e := d.Store.Endpoints(ctx)
	if e != nil {
		return Endpoint{}, e
	}
	now := time.Now().UTC()
	for _, v := range all {
		if v.Local && v.Harness == a.Harness && v.Profile == a.Profile && v.NativeSession == a.NativeSession {
			if a.Session != "" && a.Session != v.Session {
				return Endpoint{}, errors.New("native chat already has a different retained worker; no identity takeover")
			}
			if a.Name != "" {
				v.Alias = a.Name
			}
			v.UpdatedAt = now
			if a.EnableWake {
				v.Wake = true
			}
			return v, d.Store.saveValue(ctx, endpointKey(v.Device, v.Session), v)
		}
	}
	session := a.Session
	if session == "" {
		agent := a.Agent
		if agent == "" {
			agent = a.Harness
		}
		if !ValidLabel(agent) || len(agent) > 31 {
			return Endpoint{}, errors.New("agent prefix must be 1–31 label characters")
		}
		session = agent + "-" + NewID()
	}
	if !ValidLabel(session) {
		return Endpoint{}, errors.New("invalid retained worker")
	}
	for _, v := range all {
		if v.Local && v.Session == session {
			return Endpoint{}, errors.New("worker belongs to another native chat; refusing reuse")
		}
	}
	if a.Path == "" {
		h := sha256.Sum256([]byte(session + "\x00" + a.NativeSession))
		a.Path = filepath.Join(d.Home, "wake", hex.EncodeToString(h[:16])+".json")
	}
	id := NewID()
	name := a.Name
	if name == "" {
		name = defaultAlias(d.Config.Name, a.Harness, id, now)
	}
	v := Endpoint{ID: id, Device: d.Config.DeviceID, DeviceName: d.Config.Name, Session: session, Alias: name, Harness: a.Harness, Profile: a.Profile, NativeSession: a.NativeSession, Local: true, Wake: a.EnableWake, CreatedAt: now, UpdatedAt: now, Receiver: "unverified", StatePath: a.Path}
	return v, d.Store.saveValue(ctx, endpointKey(v.Device, v.Session), v)
}
func (d *Daemon) localEndpoint(ctx context.Context, session string) (Endpoint, error) {
	v, e := d.Store.Endpoint(ctx, d.Config.DeviceID, session)
	if e == nil && !v.Local {
		e = errors.New("not a local chat")
	}
	return v, e
}
func resolveEndpoint(all []Endpoint, name string) (Endpoint, error) {
	matches := []Endpoint{}
	for _, v := range all {
		if name == v.ID || name == v.Session || name == v.Alias || name == v.DeviceName+"/"+v.Alias || name == v.Device+"/"+v.ID {
			matches = append(matches, v)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return Endpoint{}, fmt.Errorf("unknown chat %q; refresh session list from the paired device first", name)
	}
	choices := []string{}
	for _, v := range matches {
		choices = append(choices, v.DeviceName+"/"+v.Alias+" ("+v.ID+")")
	}
	return Endpoint{}, fmt.Errorf("ambiguous chat %q; select exact endpoint: %s", name, strings.Join(choices, ", "))
}
func channelID(session, name string) string {
	h := sha256.Sum256([]byte(session + "\x00" + name))
	return "channel-" + hex.EncodeToString(h[:12])
}
func (d *Daemon) channel(ctx context.Context, session, name string) (string, error) {
	if !ValidLabel(name) {
		return "", errors.New("invalid channel")
	}
	bs, e := d.Store.Bindings(ctx, session)
	if e != nil {
		return "", e
	}
	ids := map[string]bool{}
	for _, b := range bs {
		if b.Channel == name || b.ChannelName == name {
			ids[b.Channel] = true
		}
	}
	if len(ids) > 1 {
		return "", errors.New("ambiguous channel name; use its full channel ID")
	}
	for id := range ids {
		return id, nil
	}
	return "", errors.New("channel not bound; use bind first")
}
func (d *Daemon) resolveChannelRecipient(ctx context.Context, a APIRequest) (APIRequest, error) {
	if a.Conversation != "" {
		return a, errors.New("channel and conversation cannot be combined")
	}
	if a.ToSession != "" {
		return a, errors.New("channel resolves the exact to_session; do not supply to-session")
	}
	if _, e := d.localEndpoint(ctx, a.Session); e != nil {
		return a, e
	}
	id, e := d.channel(ctx, a.Session, a.Channel)
	if e != nil {
		return a, e
	}
	bs, e := d.Store.Bindings(ctx, a.Session)
	if e != nil {
		return a, e
	}
	peers := []Endpoint{}
	for _, b := range bs {
		if b.Channel == id && b.Status == "accepted" {
			peer := b.Peer
			if fresh, e := d.Store.Endpoint(ctx, peer.Device, peer.Session); e == nil {
				peer = fresh
			}
			peers = append(peers, peer)
		}
	}
	var peer Endpoint
	if a.To == "" {
		if len(peers) != 1 {
			return a, errors.New("channel needs one explicit recipient; use --to or explicit --all fanout")
		}
		peer = peers[0]
	} else {
		peer, e = resolveEndpoint(peers, a.To)
		if e != nil {
			return a, e
		}
	}
	a.To = peer.Device
	a.ToSession = peer.Session
	a.Conversation = id
	a.Channel = ""
	return a, nil
}

// Local delivery turns the sender's sole stored row into a received row for the
// other chat. Only a self-addressed, locally delivered message retains that
// sender-side meaning; an arbitrary incoming record must never qualify.
func (d *Daemon) sentRecord(r Record) bool {
	m := r.Message
	return m.From == d.Config.DeviceID && (r.Direction == "out" ||
		r.Direction == "in" && r.Route == "local" && m.To == d.Config.DeviceID)
}

// Registry policy is rechecked immediately before native admission. A pause does
// not advance the cursor or discard pending notices. A leave never opens a room
// again, but an exact reply to an old outgoing task can still notify its owner.
func (d *Daemon) wakeEligible(ctx context.Context, a APIRequest) (any, error) {
	ep, e := d.localEndpoint(ctx, a.Session)
	if e != nil {
		return nil, e
	}
	if a.NativeSession != ep.NativeSession {
		return nil, errors.New("native chat mismatch")
	}
	if a.ID == "" {
		return map[string]any{"paused": ep.Paused, "wake": ep.Wake}, nil
	}
	r, e := d.Store.Get(ctx, a.ID)
	if e != nil {
		return nil, e
	}
	m := r.Message
	allowed := false
	if r.Direction == "in" && m.ToSession == ep.Session && m.To == ep.Device && m.Activity == nil && m.Pair == nil {
		bs, e := d.Store.Bindings(ctx, a.Session)
		if e != nil {
			return nil, e
		}
		for _, b := range bs {
			if b.Status == "accepted" && m.From == b.Peer.Device && m.Session == b.Peer.Session && m.Conversation == b.Channel && (m.Kind == "task" || m.Kind == "result" || m.Kind == "text" && b.TextWake) {
				allowed = true
			}
		}
		if !allowed && m.Kind == "result" && m.ReplyTo != "" {
			if old, e := d.Store.Get(ctx, m.ReplyTo); e == nil {
				q := old.Message
				allowed = d.sentRecord(old) && q.Kind == "task" && q.Session == a.Session && q.To == m.From && q.ToSession == m.Session && q.Conversation == m.Conversation
			}
		}
	}
	return map[string]any{"allowed": allowed, "paused": ep.Paused, "wake": ep.Wake}, nil
}

func (d *Daemon) channelFanout(ctx context.Context, path string, a APIRequest) (any, error) {
	if a.To != "" || a.ToSession != "" || a.Conversation != "" {
		return nil, errors.New("--all cannot be combined with to/to-session/conversation")
	}
	id, e := d.channel(ctx, a.Session, a.Channel)
	if e != nil {
		return nil, e
	}
	bs, e := d.Store.Bindings(ctx, a.Session)
	if e != nil {
		return nil, e
	}
	snapshot := []Endpoint{}
	for _, b := range bs {
		if b.Channel == id && b.Status == "accepted" {
			snapshot = append(snapshot, b.Peer)
		}
	}
	if len(snapshot) == 0 {
		return nil, errors.New("channel has no accepted recipients")
	}
	out := []map[string]any{}
	for _, peer := range snapshot {
		req := a
		req.All = false
		req.To = peer.ID
		v, e := d.dispatch(ctx, path, req)
		item := map[string]any{"endpoint": peer.ID, "device": peer.Device, "session": peer.Session}
		if e != nil {
			item["error"] = e.Error()
		} else {
			item["delivery"] = v
		}
		out = append(out, item)
	}
	return map[string]any{"recipients": out, "status": "per_recipient", "execution": "not_verified"}, nil
}
