package link

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func inboxAckKey(session, id string) string { return "inbox-ack:" + session + ":" + id }

// Handled state is local to the exact receiving worker. Reading a record never
// sets it, and acknowledging never changes a message, its lease or its history.
func (s *Store) InboxHandled(ctx context.Context, session, id string) (bool, error) {
	if !ValidLabel(session) || !ValidLabel(id) {
		return false, errors.New("handled lookup requires a retained worker and exact message ID")
	}
	var value string
	e := s.db.QueryRowContext(ctx, "SELECT value FROM metadata WHERE key=?", inboxAckKey(session, id)).Scan(&value)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	return e == nil, e
}

func (s *Store) AckInbox(ctx context.Context, device, session string, ids []string) (int64, error) {
	if !ValidLabel(session) || len(ids) < 1 || len(ids) > 32 {
		return 0, errors.New("ack requires this chat's retained worker and 1–32 exact message IDs")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	unique := map[string]bool{}
	for _, id := range ids {
		if !ValidLabel(id) {
			return 0, errors.New("invalid ack message ID")
		}
		if unique[id] {
			continue
		}
		var body, direction, status, owner string
		if e = tx.QueryRowContext(ctx, "SELECT body,direction,status,owner FROM messages WHERE id=?", id).Scan(&body, &direction, &status, &owner); e != nil {
			return 0, e
		}
		var m Message
		if e = json.Unmarshal([]byte(body), &m); e != nil {
			return 0, e
		}
		if direction != "in" || m.To != device || m.ToSession != session || m.Activity != nil || m.Pair != nil {
			return 0, errors.New("ack can only mark messages addressed to this exact local chat")
		}
		switch m.Kind {
		case "result", "text", "file":
		case "task":
			if status != "completed" || owner != session {
				return 0, errors.New("unfinished tasks cannot be acknowledged; claim and complete the task itself")
			}
		default:
			return 0, errors.New("only handled results, text, files or owned completed tasks can be acknowledged")
		}
		unique[id] = true
	}
	var added int64
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for id := range unique {
		r, e := tx.ExecContext(ctx, "INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO NOTHING", inboxAckKey(session, id), now)
		if e != nil {
			return 0, e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return 0, e
		}
		added += n
	}
	return added, tx.Commit()
}
