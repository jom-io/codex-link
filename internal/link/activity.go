package link

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Activity is sent inside the existing encrypted progress envelope.
// Worker heartbeats come only from explicit local worker calls, never a daemon timer.
type Activity struct {
	Worker     string    `json:"worker"`
	TaskID     string    `json:"task_id"`
	State      string    `json:"state"`
	Stage      string    `json:"stage,omitempty"`
	SeenAt     time.Time `json:"seen_at"`
	LeaseUntil int64     `json:"lease_until,omitempty"`
}
type ActivityView struct {
	Device string `json:"device"`
	Activity
	Health     string `json:"health"`
	AgeSeconds int64  `json:"age_seconds"`
}

func (s *Store) SaveActivity(ctx context.Context, device string, a Activity) error {
	if !ValidLabel(a.Worker) || !ValidLabel(a.TaskID) || a.SeenAt.IsZero() || len(a.Stage) > 1024 {
		return errors.New("invalid worker activity")
	}
	switch a.State {
	case "working", "waiting_user", "blocked", "completed", "stopped", "claimed":
	default:
		return errors.New("invalid worker state")
	}
	b, _ := json.Marshal(a)
	_, e := s.db.ExecContext(ctx, `INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value WHERE json_extract(excluded.value,'$.seen_at')>=json_extract(metadata.value,'$.seen_at')`, "activity:"+device+":"+a.TaskID, string(b))
	return e
}
func (s *Store) Activities(ctx context.Context, taskID string) ([]ActivityView, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT key,value FROM metadata WHERE key LIKE 'activity:%'")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ActivityView{}
	for rows.Next() {
		var key, b string
		if e = rows.Scan(&key, &b); e != nil {
			return nil, e
		}
		var a Activity
		if e = json.Unmarshal([]byte(b), &a); e != nil {
			return nil, e
		}
		if taskID != "" && a.TaskID != taskID {
			continue
		}
		age := int64(time.Since(a.SeenAt).Seconds())
		if age < 0 {
			age = 0
		}
		health := "active"
		if a.State == "completed" || a.State == "stopped" {
			health = a.State
		} else if age > 180 {
			health = "unresponsive"
		} else if age > 90 {
			health = "stale"
		}
		out = append(out, ActivityView{Device: key[len("activity:") : len(key)-len(a.TaskID)-1], Activity: a, Health: health, AgeSeconds: age})
	}
	return out, rows.Err()
}
func (d *Daemon) reportActivity(ctx context.Context, r Record, worker, state, stage string) error {
	a := Activity{Worker: worker, TaskID: r.Message.ID, State: state, Stage: stage, SeenAt: time.Now().UTC(), LeaseUntil: r.LeaseUntil}
	m := d.base(r.Message.From, "progress")
	m.Session = worker
	m.ToSession = r.Message.Session
	m.Conversation = r.Message.Conversation
	m.ReplyTo = r.Message.ID
	m.Text = "Worker status: " + state
	m.Activity = &a
	if e := d.Store.SaveActivity(ctx, d.Config.DeviceID, a); e != nil {
		return e
	}
	if e := d.Store.Enqueue(ctx, m); e != nil {
		return e
	}
	d.notify()
	return nil
}

// Only state transitions emit notifications; this never refreshes worker SeenAt.
func (d *Daemon) checkActivity(ctx context.Context) {
	views, e := d.Store.Activities(ctx, "")
	if e != nil {
		return
	}
	for _, v := range views {
		if v.Device != d.Config.DeviceID {
			continue
		}
		key := "activity-health:" + v.Device + ":" + v.TaskID
		var previous string
		d.Store.db.QueryRowContext(ctx, "SELECT value FROM metadata WHERE key=?", key).Scan(&previous)
		if previous == v.Health {
			continue
		}
		if previous != "" || v.Health == "stale" || v.Health == "unresponsive" {
			r, e := d.Store.Get(ctx, v.TaskID)
			if e != nil {
				continue
			}
			m := d.base(r.Message.From, "progress")
			m.Session = v.Worker
			m.ToSession = r.Message.Session
			m.Conversation = r.Message.Conversation
			m.ReplyTo = v.TaskID
			m.Text = "Worker health changed: " + v.Health + "; this does not prove the running process stopped"
			m.Activity = &v.Activity
			if e = d.Store.Enqueue(ctx, m); e != nil {
				continue
			}
			d.notify()
		}
		d.Store.db.ExecContext(ctx, "INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, v.Health)
	}
}
