package link

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
)

// Policy is persisted with the existing configuration; old configurations get
// conservative defaults. Times in tests are injected, never advanced in production.
type SessionPolicy struct {
	LeaseSeconds      int  `json:"lease_seconds,omitempty" yaml:"lease_seconds,omitempty"`
	TemporaryHours    int  `json:"temporary_hours,omitempty" yaml:"temporary_hours,omitempty"`
	ArchiveDays       int  `json:"archive_days,omitempty" yaml:"archive_days,omitempty"`
	CacheStaleMinutes int  `json:"cache_stale_minutes,omitempty" yaml:"cache_stale_minutes,omitempty"`
	CacheExpireDays   int  `json:"cache_expire_days,omitempty" yaml:"cache_expire_days,omitempty"`
	SweepMinutes      int  `json:"sweep_minutes,omitempty" yaml:"sweep_minutes,omitempty"`
	DisableAutoGC     bool `json:"disable_auto_gc,omitempty" yaml:"disable_auto_gc,omitempty"`
}

func (p *SessionPolicy) defaults() {
	if p.LeaseSeconds == 0 {
		p.LeaseSeconds = 300
	}
	if p.TemporaryHours == 0 {
		p.TemporaryHours = 24
	}
	if p.ArchiveDays == 0 {
		p.ArchiveDays = 7
	}
	if p.CacheStaleMinutes == 0 {
		p.CacheStaleMinutes = 10
	}
	if p.CacheExpireDays == 0 {
		p.CacheExpireDays = 30
	}
	if p.SweepMinutes == 0 {
		p.SweepMinutes = 60
	}
}
func (p SessionPolicy) validate() error {
	p.defaults()
	if p.LeaseSeconds < 120 || p.LeaseSeconds > 3600 || p.TemporaryHours < 1 || p.ArchiveDays < 1 || p.CacheStaleMinutes < 1 || p.CacheExpireDays < 1 || p.SweepMinutes < 1 || p.TemporaryHours > 87600 || p.ArchiveDays > 3650 || p.CacheExpireDays > 3650 || p.CacheStaleMinutes > 525600 || p.SweepMinutes > 10080 {
		return errors.New("invalid session lifecycle limits")
	}
	return nil
}
func (d *Daemon) sessionPolicy() SessionPolicy { p := d.Config.Sessions; p.defaults(); return p }
func (d *Daemon) sessionNow() time.Time {
	if d.lifecycleNow != nil {
		return d.lifecycleNow().UTC()
	}
	return time.Now().UTC()
}
func (d *Daemon) initLifecycle(ctx context.Context) error {
	d.registryMu.Lock()
	defer d.registryMu.Unlock()
	now := d.sessionNow()
	d.lifecycleStarted = now
	all, e := d.Store.Endpoints(ctx)
	if e != nil {
		return e
	}
	for _, ep := range all {
		if ep.Local && ep.LifecycleVersion == 0 {
			ep.LifecycleVersion = 1
			ep.LifecycleSince = now
			ep.LastUsedAt = now
			ep.Receiver = "unverified"
			if e = d.Store.saveValue(ctx, endpointKey(ep.Device, ep.Session), ep); e != nil {
				return e
			}
		}
	}
	return nil
}
func (d *Daemon) lifecycleLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	last := d.sessionNow()
	next := last.Add(time.Duration(d.sessionPolicy().SweepMinutes) * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := d.sessionNow()
			// A suspend/resume gap or backwards clock grants a fresh observation window.
			if now.Sub(last) > 2*time.Minute || now.Before(last) {
				d.registryMu.Lock()
				d.lifecycleStarted = now
				d.registryMu.Unlock()
			}
			last = now
			if now.Before(next) {
				continue
			}
			next = now.Add(time.Duration(d.sessionPolicy().SweepMinutes) * time.Minute)
			if d.sessionPolicy().DisableAutoGC {
				continue
			}
			d.registryMu.Lock()
			_, e := d.gcSessions(ctx, APIRequest{Apply: true})
			d.registryMu.Unlock()
			if e == nil {
				d.notify()
			}
		}
	}
}
func (d *Daemon) endpointView(ep Endpoint, now time.Time) Endpoint {
	if !ep.ArchivedAt.IsZero() {
		ep.Status = "archived"
		return ep
	}
	if !ep.Local {
		if ep.CacheRemoved {
			ep.Status = "withdrawn"
			return ep
		}
		if ep.FetchedAt.IsZero() {
			ep.Status = "unknown"
			return ep
		}
		age := now.Sub(ep.FetchedAt)
		p := d.sessionPolicy()
		if age > time.Duration(p.CacheExpireDays)*24*time.Hour {
			ep.Status = "expired"
		} else if age > time.Duration(p.CacheStaleMinutes)*time.Minute {
			ep.Status = "stale"
		}
		return ep
	}
	if ep.LastHostSeenAt.IsZero() {
		ep.Status = "unknown"
	} else if ep.LeaseExpiresAt.After(now) {
		ep.Status = "online"
	} else {
		ep.Status = "offline"
	}
	if ep.Status == "offline" && ep.OfflineSince.IsZero() {
		ep.OfflineSince = ep.LeaseExpiresAt
	}
	if ep.Status != "online" && ep.Receiver == "connected" {
		ep.Receiver = "unavailable"
	}
	return ep
}
func (d *Daemon) listSessions(ctx context.Context, a APIRequest) ([]Endpoint, error) {
	all, e := d.Store.Endpoints(ctx)
	if e != nil {
		return nil, e
	}
	out := []Endpoint{}
	now := d.sessionNow()
	if a.View != "" && a.View != "all" && a.View != "active" && a.View != "archived" {
		return nil, errors.New("view must be active, all or archived")
	}
	for _, ep := range all {
		ep = d.endpointView(ep, now)
		if a.View == "archived" && ep.Status != "archived" && ep.Status != "withdrawn" && ep.Status != "expired" {
			continue
		}
		if a.View == "active" && ep.Status != "online" && !(ep.Pinned && ep.Status != "archived" && ep.Status != "withdrawn" && ep.Status != "expired") {
			continue
		}
		out = append(out, ep)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if a.After < 0 {
		return nil, errors.New("after must be non-negative")
	}
	if a.After >= int64(len(out)) {
		return []Endpoint{}, nil
	}
	out = out[a.After:]
	if a.Limit > 0 && len(out) > a.Limit {
		out = out[:a.Limit]
	}
	return out, nil
}
func (d *Daemon) presence(ctx context.Context, a APIRequest) (Endpoint, error) {
	ep, e := d.localEndpoint(ctx, a.Session)
	if e != nil {
		return ep, e
	}
	if a.NativeSession != ep.NativeSession {
		return ep, errors.New("native identity mismatch")
	}
	now := d.sessionNow()
	ep.ObservedAt = now
	ep.PresenceStatus = a.State
	switch a.State {
	case "idle", "running", "waiting_user", "uncertain":
		// Presence does not restore an archived endpoint or change wake/pause rights.
		if !ep.ArchivedAt.IsZero() {
			return d.endpointView(ep, now), nil
		}
		ep.LastHostSeenAt = now
		ep.LeaseExpiresAt = now.Add(time.Duration(d.sessionPolicy().LeaseSeconds) * time.Second)
		ep.OfflineSince = time.Time{}
		ep.HostState = a.State
		ep.Receiver = "connected"
		ep.ReceiverAt = now
	case "missing", "stopped":
		ep.HostState = a.State
		ep.LeaseExpiresAt = now
		if ep.OfflineSince.IsZero() {
			ep.OfflineSince = now
		}
		ep.Receiver = "unavailable"
	case "unknown":
		// Keep any prior running/approval/uncertain protection until a real observation.
	default:
		return ep, errors.New("invalid native presence state")
	}
	e = d.Store.saveValue(ctx, endpointKey(ep.Device, ep.Session), ep)
	return d.endpointView(ep, now), e
}
func (d *Daemon) touchSession(ctx context.Context, session string) {
	d.registryMu.Lock()
	defer d.registryMu.Unlock()
	ep, e := d.localEndpoint(ctx, session)
	if e != nil {
		return
	}
	ep.LastUsedAt = d.sessionNow()
	_ = d.Store.saveValue(ctx, endpointKey(ep.Device, ep.Session), ep)
}
func (d *Daemon) checkArchivedSend(ctx context.Context, source, device, target string) error {
	for _, who := range [][2]string{{d.Config.DeviceID, source}, {device, target}} {
		if who[1] == "" {
			continue
		}
		ep, e := d.Store.Endpoint(ctx, who[0], who[1])
		if e == nil && (!ep.ArchivedAt.IsZero() || ep.CacheRemoved || ep.Status == "archived") {
			return errors.New("chat is archived or withdrawn; attach in its original native chat to restore")
		}
	}
	return nil
}

// A transaction serializes the work check and the archive write with message
// persistence. Nothing is removed, completed, transferred or acknowledged here.
func lifecycleWork(ctx context.Context, tx *sql.Tx, ep Endpoint, now time.Time) ([]string, error) {
	rows, e := tx.QueryContext(ctx, `SELECT body,direction,status,owner,lease FROM messages WHERE json_extract(body,'$.to_session')=? OR json_extract(body,'$.session')=? OR owner=?`, ep.Session, ep.Session, ep.Session)
	if e != nil {
		return nil, e
	}
	type row struct {
		m                        Message
		direction, status, owner string
		lease                    int64
	}
	rs := []row{}
	for rows.Next() {
		var r row
		var body string
		if e = rows.Scan(&body, &r.direction, &r.status, &r.owner, &r.lease); e != nil {
			rows.Close()
			return nil, e
		}
		if e = json.Unmarshal([]byte(body), &r.m); e != nil {
			rows.Close()
			return nil, e
		}
		rs = append(rs, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	blocked := map[string]bool{}
	for _, r := range rs {
		m := r.m
		if m.Activity != nil || m.Pair != nil || m.Kind == "receipt" {
			continue
		}
		if r.owner == ep.Session && r.status == "claimed" {
			blocked["unfinished-owned-task"] = true
		}
		if r.owner == ep.Session && r.status == "claimed" && r.lease > now.Unix() {
			blocked["active-task-lease"] = true
		}
		if r.direction == "in" && m.To == ep.Device && m.ToSession == ep.Session {
			if m.Kind == "task" && r.status != "completed" {
				blocked["unfinished-incoming-task"] = true
			}
			if m.Kind == "result" || m.Kind == "file" {
				var n int
				if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM metadata WHERE key=?", inboxAckKey(ep.Session, m.ID)).Scan(&n); e != nil {
					return nil, e
				}
				if n == 0 {
					blocked["unhandled-"+m.Kind] = true
				}
			}
		}
		if m.From == ep.Device && m.Session == ep.Session {
			if (m.Kind == "task" || m.Kind == "file" || m.Kind == "text") && (r.status == "pending" || r.status == "sent") {
				blocked["pending-outgoing-message"] = true
			}
			if m.Kind == "task" && r.status != "completed" {
				var n int
				e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE direction='in' AND json_extract(body,'$.kind')='result' AND json_extract(body,'$.reply_to')=? AND json_extract(body,'$.from')=? AND (COALESCE(json_extract(body,'$.session'),'')=? OR ?='') AND json_extract(body,'$.to_session')=? AND COALESCE(json_extract(body,'$.conversation'),'')=?`, m.ID, m.To, m.ToSession, m.ToSession, ep.Session, m.Conversation).Scan(&n)
				if e != nil {
					return nil, e
				}
				if n == 0 {
					blocked["awaiting-task-result"] = true
				}
			}
		}
	}
	out := []string{}
	for why := range blocked {
		out = append(out, why)
	}
	sort.Strings(out)
	return out, nil
}

type GCDecision struct {
	ID       string   `json:"id"`
	Session  string   `json:"session"`
	Alias    string   `json:"alias"`
	Reason   string   `json:"reason,omitempty"`
	Blocked  []string `json:"blocked"`
	Archived bool     `json:"archived"`
}

func (d *Daemon) archiveSession(ctx context.Context, ep Endpoint, reason string, apply, manual bool) (GCDecision, error) {
	now := d.sessionNow()
	v := GCDecision{ID: ep.ID, Session: ep.Session, Alias: ep.Alias, Reason: reason, Blocked: []string{}}
	if ep.Pinned {
		v.Blocked = append(v.Blocked, "pinned")
	}
	if ep.HostState == "running" || ep.HostState == "waiting_user" || ep.HostState == "uncertain" {
		v.Blocked = append(v.Blocked, "native-"+ep.HostState)
	}
	if !manual && ep.LeaseExpiresAt.After(now) {
		v.Blocked = append(v.Blocked, "online-lease")
	}
	if manual && (ep.ObservedAt.IsZero() || now.Sub(ep.ObservedAt) > 5*time.Second || ep.PresenceStatus == "unknown") {
		v.Blocked = append(v.Blocked, "fresh-native-observation-required")
	}
	bs, e := d.Store.Bindings(ctx, ep.Session)
	if e != nil {
		return v, e
	}
	if (len(bs) > 0 || !ep.ReceiverAt.IsZero()) && (ep.PresenceStatus == "unknown" || ep.HostState == "") {
		v.Blocked = append(v.Blocked, "native-state-unconfirmed")
	}
	if ep.StatePath != "" {
		b, e := os.ReadFile(ep.StatePath)
		if e == nil {
			var q struct {
				Pending []json.RawMessage `json:"pending"`
			}
			if json.Unmarshal(b, &q) != nil {
				v.Blocked = append(v.Blocked, "unreadable-wake-state")
			} else if len(q.Pending) > 0 {
				v.Blocked = append(v.Blocked, "pending-wake")
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			v.Blocked = append(v.Blocked, "unreadable-wake-state")
		}
	}
	if ep.Harness == "codex" && ep.StatePath != "" {
		b, err := os.ReadFile(ep.StatePath + ".codex-submissions.json")
		if err == nil {
			var journal struct {
				Session string `json:"session"`
				Native  string `json:"nativeSessionId"`
				Entries []struct {
					Status string `json:"status"`
				} `json:"entries"`
			}
			if json.Unmarshal(b, &journal) != nil || journal.Session != ep.Session || journal.Native != ep.NativeSession {
				v.Blocked = append(v.Blocked, "unreadable-admission-journal")
			} else {
				for _, entry := range journal.Entries {
					if entry.Status == "intent" || entry.Status != "accepted" && entry.Status != "rejected" {
						v.Blocked = append(v.Blocked, "uncertain-admission-journal")
						break
					}
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			v.Blocked = append(v.Blocked, "unreadable-admission-journal")
		}
	}
	tx, e := d.Store.db.BeginTx(ctx, nil)
	if e != nil {
		return v, e
	}
	defer tx.Rollback()
	work, e := lifecycleWork(ctx, tx, ep, now)
	if e != nil {
		return v, e
	}
	v.Blocked = append(v.Blocked, work...)
	if len(v.Blocked) > 0 || !apply || reason == "" {
		return v, nil
	}
	ep.ArchivedAt = now
	ep.ArchiveReason = reason
	ep.Status = ""
	b, e := json.Marshal(ep)
	if e != nil {
		return v, e
	}
	_, e = tx.ExecContext(ctx, "UPDATE metadata SET value=? WHERE key=?", string(b), endpointKey(ep.Device, ep.Session))
	if e != nil {
		return v, e
	}
	e = tx.Commit()
	v.Archived = e == nil
	return v, e
}
func (d *Daemon) gcSessions(ctx context.Context, a APIRequest) ([]GCDecision, error) {
	all, e := d.Store.Endpoints(ctx)
	if e != nil {
		return nil, e
	}
	out := []GCDecision{}
	now := d.sessionNow()
	p := d.sessionPolicy()
	for _, ep := range all {
		if !ep.Local || !ep.ArchivedAt.IsZero() || a.Session != "" && ep.Session != a.Session {
			continue
		}
		view := d.endpointView(ep, now)
		bs, e := d.Store.Bindings(ctx, ep.Session)
		if e != nil {
			return nil, e
		}
		reason := ""
		if len(bs) == 0 && ep.LastHostSeenAt.IsZero() && ep.ReceiverAt.IsZero() && !ep.LastUsedAt.IsZero() && now.Sub(ep.LastUsedAt) >= time.Duration(p.TemporaryHours)*time.Hour {
			reason = "unused-temporary"
		}
		if (view.Status == "offline" || view.Status == "unknown") && !ep.LastUsedAt.IsZero() && now.Sub(ep.LastUsedAt) >= time.Duration(p.ArchiveDays)*24*time.Hour {
			since := view.OfflineSince
			if since.IsZero() {
				since = ep.LifecycleSince
			}
			if !since.IsZero() && now.Sub(since) >= time.Duration(p.ArchiveDays)*24*time.Hour {
				reason = "long-offline"
			}
		}
		if reason == "" {
			continue
		}
		grace := ep.LifecycleSince.IsZero() || now.Sub(ep.LifecycleSince) < 5*time.Minute || !d.lifecycleStarted.IsZero() && now.Sub(d.lifecycleStarted) < 5*time.Minute
		v, e := d.archiveSession(ctx, ep, reason, a.Apply && !grace, false)
		if e != nil {
			return nil, e
		}
		if grace {
			v.Blocked = append(v.Blocked, "reconnect-grace")
		}
		// Apply is deferred until the grace check too.
		out = append(out, v)
	}
	return out, nil
}
func (d *Daemon) lifecycleDispatch(ctx context.Context, path string, a APIRequest) (any, error) {
	if path == "/v1/session/gc" {
		return d.gcSessions(ctx, a)
	}
	if path == "/v1/session/presence" {
		return d.presence(ctx, a)
	}
	ep, e := d.localEndpoint(ctx, a.Session)
	if e != nil {
		return nil, e
	}
	switch path {
	case "/v1/session/pin", "/v1/session/unpin":
		ep.Pinned = path == "/v1/session/pin"
		e = d.Store.saveValue(ctx, endpointKey(ep.Device, ep.Session), ep)
		return d.endpointView(ep, d.sessionNow()), e
	case "/v1/session/detach":
		if !ep.ArchivedAt.IsZero() {
			return d.endpointView(ep, d.sessionNow()), nil
		}
		v, e := d.archiveSession(ctx, ep, "manual", true, true)
		if e != nil {
			return nil, e
		}
		if !v.Archived {
			return nil, fmt.Errorf("chat is protected: %v", v.Blocked)
		}
		d.notify()
		return v, nil
	}
	return nil, errors.New("unknown lifecycle operation")
}
