package link

import (
	"context"
	"database/sql"
)

// Keep the old schema/rows intact; older binaries ignore these metadata keys.
const recordSelect = "SELECT seq,body,direction,status,owner,lease,result,COALESCE((SELECT value FROM metadata WHERE key='route:'||messages.id),'') FROM messages"

func (s *Store) metadata(ctx context.Context, key string) string {
	var value string
	s.db.QueryRowContext(ctx, "SELECT value FROM metadata WHERE key=?", key).Scan(&value)
	return value
}

func (s *Store) setMetadata(ctx context.Context, key, value string) error {
	_, e := s.db.ExecContext(ctx, "INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value)
	return e
}

func (s *Store) setRoute(ctx context.Context, id, route string) error {
	return s.setMetadata(ctx, "route:"+id, route)
}

// One shared database holds one copy of a local message. After any file has
// reached its immutable cache, turn the queued row into an ordinary inbox row.
// Existing claim/complete transactions then apply without a second task ID.
func (s *Store) deliverLocal(ctx context.Context, id string) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	res, e := tx.ExecContext(ctx, "UPDATE messages SET direction='in',status='received' WHERE id=? AND direction='out' AND status='pending' AND json_extract(body,'$.from')=json_extract(body,'$.to')", id)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO metadata(key,value) VALUES(?, 'local') ON CONFLICT(key) DO UPDATE SET value='local'", "route:"+id); e != nil {
		return e
	}
	return tx.Commit()
}
