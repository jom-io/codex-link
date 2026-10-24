package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

var lifecycleFields = []string{"lifecycle_version", "lifecycle_since", "last_host_seen_at", "lease_expires_at", "offline_since", "last_used_at", "host_state", "presence_status", "observed_at", "archived_at", "archive_reason", "pinned", "status", "fetched_at", "cache_removed"}

// v1 peers use DisallowUnknownFields, including nested Endpoint fields. Strip
// lifecycle fields on legacy frames rather than assuming additive JSON is safe.
func (c sessionControl) MarshalJSON() ([]byte, error) {
	type plain sessionControl
	b, e := json.Marshal(plain(c))
	if e != nil {
		return nil, e
	}
	var wire map[string]any
	if e = json.Unmarshal(b, &wire); e != nil {
		return nil, e
	}
	{
		strip := func(ep any) {
			if m, ok := ep.(map[string]any); ok {
				for _, k := range lifecycleFields {
					if c.Op == "directory_page" && (k == "lifecycle_version" || k == "status" || k == "pinned") {
						continue
					}
					delete(m, k)
				}
			}
		}
		if eps, ok := wire["endpoints"].([]any); ok {
			for _, ep := range eps {
				strip(ep)
			}
		}
		strip(wire["source"])
	}
	return json.Marshal(wire)
}
func (d *Daemon) replyDirectory(ctx context.Context, m Message) error {
	all, e := d.Store.Endpoints(ctx)
	if e != nil {
		return e
	}
	now := d.sessionNow()
	eps := []Endpoint{}
	for _, ep := range all {
		if ep.Local && ep.ArchivedAt.IsZero() {
			v := publicEndpoint(ep)
			v.LifecycleVersion = 1
			v.Status = d.endpointView(ep, now).Status
			v.Pinned = ep.Pinned
			eps = append(eps, v)
		}
	}
	sort.Slice(eps, func(i, j int) bool { return eps[i].ID < eps[j].ID })
	legacy := eps
	if len(legacy) > 64 {
		legacy = legacy[:64]
	}
	if _, e = d.control(ctx, m.From, "", "", sessionControl{Op: "directory", Request: m.ID, Endpoints: legacy}); e != nil {
		return e
	}
	pages := (len(eps) + 63) / 64
	if pages == 0 {
		pages = 1
	}
	snapshot := NewID()
	for i := 0; i < pages; i++ {
		end := (i + 1) * 64
		if end > len(eps) {
			end = len(eps)
		}
		if _, e = d.control(ctx, m.From, "", "", sessionControl{Op: "directory_page", Request: m.ID, Snapshot: snapshot, Page: i, Pages: pages, Endpoints: eps[i*64 : end]}); e != nil {
			return e
		}
	}
	return nil
}
func (d *Daemon) directoryRequest(ctx context.Context, m Message, c sessionControl) bool {
	q, e := d.Store.Get(ctx, c.Request)
	if e != nil || q.Direction != "out" || q.Message.To != m.From {
		return false
	}
	query, e := parseControl(q.Message)
	if e != nil || query.Op != "query" {
		return false
	}
	latest := d.Store.metadata(ctx, "directory-latest:"+m.From)
	return latest == "" || latest == c.Request
}
func (d *Daemon) receiveLegacyDirectory(ctx context.Context, m Message, c sessionControl) error {
	if !d.directoryRequest(ctx, m, c) {
		return nil
	}
	if d.Store.metadata(ctx, "directory-applied:"+m.From) == c.Request {
		return nil
	}
	for _, ep := range c.Endpoints {
		if ep.Device == d.Config.DeviceID {
			continue
		}
		prior, e := d.Store.Endpoint(ctx, ep.Device, ep.Session)
		if e == nil && prior.LifecycleVersion > 0 {
			continue
		}
		ep.FetchedAt = d.sessionNow()
		ep.Status = "unknown"
		if e = d.Store.saveValue(ctx, endpointKey(ep.Device, ep.Session), ep); e != nil {
			return e
		}
	}
	return d.Store.setMetadata(ctx, "directory-ready:"+m.From, m.ID)
}
func (d *Daemon) receiveDirectoryPage(ctx context.Context, m Message, c sessionControl) error {
	if m.From == d.Config.DeviceID {
		return nil
	}
	if !d.directoryRequest(ctx, m, c) || !ValidLabel(c.Snapshot) || c.Pages < 1 || c.Pages > 1024 || c.Page < 0 || c.Page >= c.Pages {
		return nil
	}
	if d.Store.metadata(ctx, "directory-applied:"+m.From) == c.Request {
		return nil
	}
	base := "directory-parts:" + m.From + ":" + c.Request + ":"
	header := fmt.Sprintf("%s:%d", c.Snapshot, c.Pages)
	old := d.Store.metadata(ctx, base+"header")
	if old != "" && old != header {
		return errors.New("directory snapshot header mismatch")
	}
	for _, ep := range c.Endpoints {
		if ep.LifecycleVersion != 1 || ep.Status != "online" && ep.Status != "offline" && ep.Status != "unknown" {
			return errors.New("invalid directory lifecycle view")
		}
	}
	if e := d.Store.setMetadata(ctx, base+"header", header); e != nil {
		return e
	}
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	key := base + fmt.Sprint(c.Page)
	if prior := d.Store.metadata(ctx, key); prior != "" && prior != string(b) {
		return errors.New("conflicting directory page")
	}
	if e = d.Store.setMetadata(ctx, key, string(b)); e != nil {
		return e
	}
	eps := []Endpoint{}
	seen := map[string]bool{}
	seenIDs := map[string]bool{}
	for i := 0; i < c.Pages; i++ {
		b := d.Store.metadata(ctx, base+fmt.Sprint(i))
		if b == "" {
			return nil
		}
		var part sessionControl
		if e = json.Unmarshal([]byte(b), &part); e != nil {
			return e
		}
		if part.Snapshot != c.Snapshot || part.Pages != c.Pages || part.Page != i {
			return errors.New("inconsistent directory page")
		}
		for _, ep := range part.Endpoints {
			if seen[ep.Session] || seenIDs[ep.ID] {
				return errors.New("duplicate directory endpoint")
			}
			seen[ep.Session] = true
			seenIDs[ep.ID] = true
			eps = append(eps, ep)
		}
	}
	oldEndpoints, e := d.Store.Endpoints(ctx)
	if e != nil {
		return e
	}
	now := d.sessionNow()
	tx, e := d.Store.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	put := func(k string, v any) error {
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, string(b))
		return e
	}
	for _, ep := range oldEndpoints {
		if !ep.Local && ep.Device == m.From && !seen[ep.Session] {
			ep.CacheRemoved = true
			if e = put(endpointKey(ep.Device, ep.Session), ep); e != nil {
				return e
			}
		}
	}
	for _, ep := range eps {
		ep.Local = false
		ep.FetchedAt = now
		ep.CacheRemoved = false
		if e = put(endpointKey(ep.Device, ep.Session), ep); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", "directory-applied:"+m.From, c.Request); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM metadata WHERE key LIKE ?", base+"%"); e != nil {
		return e
	}
	return tx.Commit()
}

// Local timestamps and native state must never be imported from a peer directory.
func publicLifecycleValid(ep Endpoint) bool {
	return ep.LifecycleSince.IsZero() && ep.LastHostSeenAt.IsZero() && ep.LeaseExpiresAt.IsZero() && ep.OfflineSince.IsZero() && ep.LastUsedAt.IsZero() && ep.ObservedAt.IsZero() && ep.ArchivedAt.IsZero() && ep.FetchedAt.IsZero() && ep.HostState == "" && ep.PresenceStatus == "" && ep.ArchiveReason == "" && !ep.CacheRemoved
}
