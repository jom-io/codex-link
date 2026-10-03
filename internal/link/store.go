package link

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func OpenStore(home string) (*Store, error) {
	if e := os.MkdirAll(home, 0700); e != nil {
		return nil, e
	}
	p := filepath.Join(home, "state.db")
	f, e := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	db, e := sql.Open("sqlite", p)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS messages(seq INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT NOT NULL UNIQUE,body TEXT NOT NULL,direction TEXT NOT NULL,status TEXT NOT NULL,owner TEXT NOT NULL DEFAULT '',lease INTEGER NOT NULL DEFAULT 0,result TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS metadata(key TEXT PRIMARY KEY,value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS pairs(peer_id TEXT PRIMARY KEY,body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS files(message_id TEXT PRIMARY KEY,path TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS inbox ON messages(direction,seq);
CREATE INDEX IF NOT EXISTS outbox ON messages(direction,status);`)
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Enqueue(ctx context.Context, m Message) error {
	if e := m.Validate(); e != nil {
		return e
	}
	b, _ := json.Marshal(m)
	_, e := s.db.ExecContext(ctx, "INSERT INTO messages(id,body,direction,status) VALUES(?,?,'out','pending')", m.ID, string(b))
	return e
}
func (s *Store) Cursor(ctx context.Context) (string, error) {
	var v string
	e := s.db.QueryRowContext(ctx, "SELECT value FROM metadata WHERE key='cursor'").Scan(&v)
	if errors.Is(e, sql.ErrNoRows) {
		return "0-0", nil
	}
	return v, e
}
func (s *Store) Accept(ctx context.Context, m *Message, cursor string, receipt *Message) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if m != nil {
		b, _ := json.Marshal(m)
		res, e := tx.ExecContext(ctx, "INSERT OR IGNORE INTO messages(id,body,direction,status) VALUES(?,?,'in','received')", m.ID, string(b))
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n > 0 && m.Kind == "receipt" {
			if _, e = tx.ExecContext(ctx, "UPDATE messages SET status='delivered' WHERE id=? AND direction='out' AND json_extract(body,'$.to')=?", m.ReplyTo, m.From); e != nil {
				return e
			}
		}
		if n > 0 && receipt != nil {
			b, _ := json.Marshal(receipt)
			if _, e = tx.ExecContext(ctx, "INSERT INTO messages(id,body,direction,status) VALUES(?,?,'out','pending')", receipt.ID, string(b)); e != nil {
				return e
			}
		}
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO metadata(key,value) VALUES('cursor',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", cursor); e != nil {
		return e
	}
	return tx.Commit()
}
func scanRecords(rows *sql.Rows) ([]Record, error) {
	out := []Record{}
	for rows.Next() {
		var r Record
		var body string
		if e := rows.Scan(&r.Seq, &body, &r.Direction, &r.Status, &r.Owner, &r.LeaseUntil, &r.Result); e != nil {
			return nil, e
		}
		if e := json.Unmarshal([]byte(body), &r.Message); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) List(ctx context.Context, after int64, limit int, history bool, session, conversation string) ([]Record, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	q := "SELECT seq,body,direction,status,owner,lease,result FROM messages WHERE seq>? AND json_extract(body,'$.kind')!='receipt' AND json_extract(body,'$.kind') NOT LIKE 'pair_%'"
	args := []any{after}
	if !history {
		q += " AND direction='in'"
	}
	if session != "" {
		q += " AND (COALESCE(json_extract(body,'$.to_session'),'')='' OR json_extract(body,'$.to_session')=?)"
		args = append(args, session)
	}
	if conversation != "" {
		q += " AND json_extract(body,'$.conversation')=?"
		args = append(args, conversation)
	}
	q += " ORDER BY seq LIMIT ?"
	args = append(args, limit)
	rows, e := s.db.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	return scanRecords(rows)
}
func (s *Store) ListTasks(ctx context.Context, after int64, limit int, session, conversation string) ([]Record, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	q := "SELECT seq,body,direction,status,owner,lease,result FROM messages WHERE seq>? AND direction='in' AND json_extract(body,'$.kind')='task'"
	args := []any{after}
	if session != "" {
		q += " AND (COALESCE(json_extract(body,'$.to_session'),'')='' OR json_extract(body,'$.to_session')=?)"
		args = append(args, session)
	}
	if conversation != "" {
		q += " AND json_extract(body,'$.conversation')=?"
		args = append(args, conversation)
	}
	q += " ORDER BY seq LIMIT ?"
	args = append(args, limit)
	rows, e := s.db.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	return scanRecords(rows)
}
func (s *Store) Pending(ctx context.Context) ([]Record, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT seq,body,direction,status,owner,lease,result FROM messages WHERE direction='out' AND status='pending' ORDER BY CASE WHEN json_extract(body,'$.kind') LIKE 'pair_%' THEN 0 ELSE 1 END,seq LIMIT 100")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	return scanRecords(rows)
}
func (s *Store) MarkSent(ctx context.Context, id string) error {
	_, e := s.db.ExecContext(ctx, "UPDATE messages SET status='sent' WHERE id=? AND status='pending'", id)
	return e
}
func (s *Store) Get(ctx context.Context, id string) (Record, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT seq,body,direction,status,owner,lease,result FROM messages WHERE id=?", id)
	if e != nil {
		return Record{}, e
	}
	rs, e := scanRecords(rows)
	rows.Close()
	if e != nil {
		return Record{}, e
	}
	if len(rs) == 0 {
		return Record{}, errors.New("message not found")
	}
	return rs[0], nil
}
func (s *Store) MaxSeq(ctx context.Context) (int64, error) {
	var n int64
	e := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(seq),0) FROM messages").Scan(&n)
	return n, e
}
func (s *Store) Claim(ctx context.Context, id, owner string, seconds int) error {
	if !ValidLabel(owner) || seconds < 10 || seconds > 86400 {
		return errors.New("valid session and lease of 10–86400 seconds required")
	}
	now := time.Now().Unix()
	res, e := s.db.ExecContext(ctx, `UPDATE messages SET owner=?,lease=?,status='claimed' WHERE id=? AND direction='in' AND json_extract(body,'$.kind')='task' AND status!='completed' AND (owner='' OR lease<=? OR owner=?)`, owner, now+int64(seconds), id, now, owner)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		r, lookupErr := s.Get(ctx, id)
		if lookupErr != nil {
			return errors.New("task not found; use the exact message.id from `task list` or `inbox` (do not use reply_to)")
		}
		if r.Direction != "in" {
			return errors.New("cannot claim an outgoing message; use the incoming task's message.id")
		}
		if r.Message.Kind != "task" {
			return errors.New("message is not a task; use the task's own message.id, not reply_to")
		}
		if r.Status == "completed" {
			return errors.New("task is already completed")
		}
		if r.Status == "claimed" && r.LeaseUntil > now && r.Owner != owner {
			return fmt.Errorf("task is claimed by session %q until %s; use a unique --session for each Codex window, or wait for the lease to expire", r.Owner, time.Unix(r.LeaseUntil, 0).UTC().Format(time.RFC3339))
		}
		return errors.New("task could not be claimed; refresh `task list` and use its exact message.id")
	}
	return nil
}

// Completing a task and queuing its response are one transaction, so a crash cannot lose the result.
func (s *Store) Complete(ctx context.Context, id, owner, result string, reply Message) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	res, e := tx.ExecContext(ctx, "UPDATE messages SET status='completed',result=?,lease=0 WHERE id=? AND direction='in' AND status='claimed' AND owner=? AND lease>?", result, id, owner, time.Now().Unix())
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("task must have an active lease owned by this session")
	}
	if e = reply.Validate(); e != nil {
		return e
	}
	b, _ := json.Marshal(reply)
	if _, e = tx.ExecContext(ctx, "INSERT INTO messages(id,body,direction,status) VALUES(?,?,'out','pending')", reply.ID, string(b)); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) SetFile(ctx context.Context, id, path string) error {
	_, e := s.db.ExecContext(ctx, "INSERT INTO files(message_id,path) VALUES(?,?) ON CONFLICT(message_id) DO UPDATE SET path=excluded.path", id, path)
	return e
}
