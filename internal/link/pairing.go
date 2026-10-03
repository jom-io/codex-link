package link

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"runtime"
	"time"
)

type PairOffer struct {
	Peer                 Peer   `json:"peer"`
	TargetSignPublic     string `json:"target_sign_public"`
	TargetExchangePublic string `json:"target_exchange_public"`
	Nonce                string `json:"nonce"`
	RequestID            string `json:"request_id"`
}
type Pair struct {
	ID             string `json:"id"`
	Peer           Peer   `json:"peer"`
	Nonce          string `json:"nonce"`
	Code           string `json:"code"`
	LocalApproved  bool   `json:"local_approved"`
	RemoteApproved bool   `json:"remote_approved"`
	Rejected       bool   `json:"rejected"`
	ExpiresAt      int64  `json:"expires_at"`
}

func (p Pair) Ready() bool   { return p.LocalApproved && p.RemoteApproved && !p.Rejected }
func (p Pair) Pending() bool { return !p.Rejected && !p.Ready() && p.ExpiresAt > time.Now().Unix() }
func (s *Store) Pair(ctx context.Context, id string) (Pair, error) {
	var p Pair
	var b string
	e := s.db.QueryRowContext(ctx, "SELECT body FROM pairs WHERE peer_id=?", id).Scan(&b)
	if e != nil {
		return p, e
	}
	e = json.Unmarshal([]byte(b), &p)
	return p, e
}
func (s *Store) Pairs(ctx context.Context) ([]Pair, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT body FROM pairs ORDER BY peer_id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Pair{}
	for rows.Next() {
		var b string
		var p Pair
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &p); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) SavePair(ctx context.Context, p Pair, out *Message) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	b, _ := json.Marshal(p)
	if _, e = tx.ExecContext(ctx, "INSERT INTO pairs(peer_id,body) VALUES(?,?) ON CONFLICT(peer_id) DO UPDATE SET body=excluded.body", p.Peer.ID, string(b)); e != nil {
		return e
	}
	if out != nil {
		if e = out.Validate(); e != nil {
			return e
		}
		b, _ := json.Marshal(out)
		if _, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO messages(id,body,direction,status) VALUES(?,?,'out','pending')", out.ID, string(b)); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (d *Daemon) localPeer() Peer { return d.Transport.LocalPeer() }
func (d *Daemon) offer(p Pair) *PairOffer {
	return &PairOffer{Peer: d.localPeer(), TargetSignPublic: p.Peer.SignPublic, TargetExchangePublic: p.Peer.ExchangePublic, Nonce: p.Nonce, RequestID: p.ID}
}
func (d *Daemon) ensurePair(ctx context.Context, to string) (Pair, error) {
	d.pairMu.Lock()
	defer d.pairMu.Unlock()
	p, e := d.Store.Pair(ctx, to)
	if e == nil && (p.Ready() || p.Pending()) {
		return p, nil
	}
	ps, e := d.Transport.Peers(ctx)
	if e != nil {
		return Pair{}, e
	}
	var peer Peer
	for _, v := range ps {
		if v.ID == to {
			peer = v
		}
	}
	if peer.ID == "" {
		return Pair{}, errors.New("new peer must register before pairing")
	}
	if e == nil && p.Peer.SignPublic != "" && p.Peer.SignPublic != peer.SignPublic {
		return Pair{}, errors.New("peer identity changed; pairing refused")
	}
	p = Pair{ID: NewID(), Peer: peer, Nonce: NewID(), ExpiresAt: time.Now().Add(10 * time.Minute).Unix()}
	p.Code = pairCode(d.localPeer(), peer, p.Nonce)
	m := d.base(to, "pair_request")
	m.ID = p.ID
	m.Pair = d.offer(p)
	if e = d.Store.SavePair(ctx, p, &m); e != nil {
		return p, e
	}
	d.notify()
	return p, nil
}
func (d *Daemon) receivePair(ctx context.Context, m Message) error {
	d.pairMu.Lock()
	defer d.pairMu.Unlock()
	o := m.Pair
	if o == nil || !o.Peer.Verify() || o.Peer.ID != m.From || o.TargetSignPublic != d.localPeer().SignPublic || o.TargetExchangePublic != d.localPeer().ExchangePublic {
		return errors.New("invalid pairing target")
	}
	p, e := d.Store.Pair(ctx, m.From)
	if m.Kind == "pair_request" {
		if time.Since(m.CreatedAt) > 10*time.Minute || m.CreatedAt.After(time.Now().Add(time.Minute)) {
			return errors.New("expired pairing request")
		}

		if e == nil && p.Pending() && p.ID != o.RequestID && p.ID < o.RequestID {
			return nil
		}
		if e == nil && p.ID == o.RequestID {
			return nil
		}
		pairs, e := d.Store.Pairs(ctx)
		if e != nil {
			return e
		}
		pending := 0
		for _, v := range pairs {
			if v.Pending() {
				pending++
			}
		}
		if pending >= 10 {
			return errors.New("too many pairing requests")
		}
		p = Pair{ID: o.RequestID, Peer: o.Peer, Nonce: o.Nonce, ExpiresAt: m.CreatedAt.Add(10 * time.Minute).Unix()}
		p.Code = pairCode(d.localPeer(), p.Peer, p.Nonce)
	} else {
		if e != nil || !p.Pending() || p.ID != o.RequestID || p.Nonce != o.Nonce || p.Peer.SignPublic != o.Peer.SignPublic || p.Peer.ExchangePublic != o.Peer.ExchangePublic {
			return errors.New("unexpected pairing decision")
		}
		if m.Kind == "pair_accept" {
			p.RemoteApproved = true
		} else if m.Kind == "pair_reject" {
			p.Rejected = true
		} else {
			return errors.New("invalid pairing decision")
		}
	}
	if e = d.Store.SavePair(ctx, p, nil); e != nil {
		return e
	}
	d.notify()
	return nil
}
func (d *Daemon) decidePair(ctx context.Context, id, code string, accept bool) (Pair, error) {
	d.pairMu.Lock()
	defer d.pairMu.Unlock()
	pairs, e := d.Store.Pairs(ctx)
	if e != nil {
		return Pair{}, e
	}
	for _, p := range pairs {
		if p.ID != id {
			continue
		}
		if !p.Pending() {
			return p, errors.New("pairing no longer pending")
		}
		if code != p.Code {
			return p, errors.New("pairing verification code mismatch")
		}
		if accept {
			p.LocalApproved = true
		} else {
			p.Rejected = true
		}
		kind := "pair_accept"
		if !accept {
			kind = "pair_reject"
		}
		m := d.base(p.Peer.ID, kind)
		m.Pair = d.offer(p)
		if e = d.Store.SavePair(ctx, p, &m); e != nil {
			return p, e
		}
		d.notify()
		return p, nil
	}
	return Pair{}, errors.New("pairing request not found")
}
func approveNative(ctx context.Context, p Pair) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	script := `on run argv
set answer to display dialog ("Pair codex-link with " & item 1 of argv & "?" & return & "Compare this code on BOTH Macs: " & item 2 of argv & return & "Device: " & item 3 of argv & return & "Only confirm if you intended this connection.") with title "codex-link pairing" buttons {"Reject", "Confirm"} default button "Reject" giving up after 120
if gave up of answer then return "timeout"
return button returned of answer
end run`
	b, e := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script, p.Peer.Name, p.Code, p.Peer.ID).Output()
	return e == nil && string(b) == "Confirm\n"
}
func (d *Daemon) prompts(ctx context.Context) {
	if d.Config.PairingHeadless {
		return
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	shown := map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			ps, e := d.Store.Pairs(ctx)
			if e != nil {
				continue
			}
			for _, p := range ps {
				if !p.Pending() || p.LocalApproved || shown[p.ID] {
					continue
				}
				shown[p.ID] = true
				accept := approveNative(ctx, p)
				if ctx.Err() != nil {
					return
				}
				if accept {
					d.decidePair(ctx, p.ID, p.Code, true)
				} else {
					d.decidePair(ctx, p.ID, p.Code, false)
				}
			}
		}
	}
}
