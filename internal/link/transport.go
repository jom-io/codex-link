package link

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrNotPaired = errors.New("peer is not mutually confirmed yet")

type Transport struct {
	client   *redis.Client
	config   Config
	identity *Identity
	store    *Store
	prefix   string
}

func NewTransport(c Config, s Secrets) (*Transport, error) {
	identity, e := EnsureIdentity(&s)
	if e != nil {
		return nil, e
	}
	if identity.ID() != c.DeviceID {
		return nil, errors.New("device identity mismatch")
	}
	o := &redis.Options{Addr: c.Redis.Address, Username: c.Redis.Username, Password: s.RedisPassword, DB: c.Redis.DB, DialTimeout: 5 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 5 * time.Second, MaxRetries: 1, ContextTimeoutEnabled: true}
	if c.Redis.TLS {
		o.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	hash := sha256.Sum256([]byte(c.Workspace))
	return &Transport{client: redis.NewClient(o), config: c, identity: identity, prefix: "cl:v2:{" + hex.EncodeToString(hash[:8]) + "}:"}, nil
}
func (t *Transport) Close() error                   { return t.client.Close() }
func (t *Transport) Ping(ctx context.Context) error { return t.client.Ping(ctx).Err() }
func (t *Transport) Inbox(id string) string         { return t.prefix + "inbox:" + id }
func (t *Transport) LocalPeer() Peer {
	p := Peer{ID: t.config.DeviceID, Name: t.config.Name, Protocol: ProtocolVersion, SeenAt: time.Now().UTC(), SignPublic: t.identity.SignPublic(), ExchangePublic: t.identity.ExchangePublic()}
	p.Signature = t.identity.Sign(p.signedBytes())
	return p
}
func (t *Transport) PairKey(ctx context.Context, id string) (string, error) {
	if t.store == nil {
		return "", ErrNotPaired
	}
	p, e := t.store.Pair(ctx, id)
	if e != nil || !p.Ready() {
		return "", ErrNotPaired
	}
	return t.identity.PairKey(t.config.Workspace+"/"+p.Nonce, p.Peer)
}

// The dedup marker and stream append are atomic; retries after a local crash reuse the ID.
var publishScript = redis.NewScript(`
local prev=redis.call('GET',KEYS[2])
if prev then return prev end
local id=redis.call('XADD',KEYS[1],'MAXLEN','~',ARGV[2],'*','envelope',ARGV[1])
redis.call('EXPIRE',KEYS[1],ARGV[3])
redis.call('SET',KEYS[2],id,'EX',ARGV[3])
return id`)

func (t *Transport) Publish(ctx context.Context, m Message) error {
	if e := m.Validate(); e != nil {
		return e
	}
	b, _ := json.Marshal(m)
	w := Wire{From: m.From, To: m.To, Public: t.identity.SignPublic()}
	if strings.HasPrefix(m.Kind, "pair_") {
		w.Plain = true
		w.Payload = string(b)
	} else {
		key, e := t.PairKey(ctx, m.To)
		if e != nil {
			return e
		}
		codec, e := NewCodec(t.config.Workspace, key)
		if e != nil {
			return e
		}
		w.Payload, e = codec.Seal(b)
		if e != nil {
			return e
		}
	}
	w.Signature = t.identity.Sign(w.signedBytes())
	payload, _ := json.Marshal(w)
	return publishScript.Run(ctx, t.client, []string{t.Inbox(m.To), t.prefix + "sent:" + m.To + ":" + m.ID}, string(payload), t.config.StreamMaxLen, t.config.StreamTTLHours*3600).Err()
}
func (t *Transport) Read(ctx context.Context, cursor string) ([]redis.XMessage, error) {
	streams, e := t.client.XRead(ctx, &redis.XReadArgs{Streams: []string{t.Inbox(t.config.DeviceID), cursor}, Count: 100, Block: 25 * time.Second}).Result()
	if errors.Is(e, redis.Nil) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if len(streams) == 0 {
		return nil, nil
	}
	return streams[0].Messages, nil
}
func (t *Transport) Decode(raw redis.XMessage) (Message, error) {
	var m Message
	payload, ok := raw.Values["envelope"].(string)
	if !ok || len(payload) > 256*1024 {
		return m, errors.New("invalid envelope")
	}
	var w Wire
	if e := json.Unmarshal([]byte(payload), &w); e != nil {
		return m, e
	}
	if w.To != t.config.DeviceID || w.From != identityID(w.Public) || !verifySignature(w.Public, w.signedBytes(), w.Signature) {
		return m, errors.New("invalid sender signature")
	}
	var b []byte
	if w.Plain {
		b = []byte(w.Payload)
	} else {
		if t.store == nil {
			return m, ErrNotPaired
		}
		p, e := t.store.Pair(context.Background(), w.From)
		if e != nil || !p.Ready() || p.Peer.SignPublic != w.Public {
			return m, ErrNotPaired
		}
		key, e := t.identity.PairKey(t.config.Workspace+"/"+p.Nonce, p.Peer)
		if e != nil {
			return m, e
		}
		codec, e := NewCodec(t.config.Workspace, key)
		if e != nil {
			return m, e
		}
		b, e = codec.Open(w.Payload)
		if e != nil {
			return m, e
		}
	}
	if e := json.Unmarshal(b, &m); e != nil {
		return m, e
	}
	if e := m.Validate(); e != nil {
		return m, e
	}
	if m.To != w.To || m.From != w.From || w.Plain != strings.HasPrefix(m.Kind, "pair_") {
		return m, errors.New("invalid envelope routing")
	}
	return m, nil
}
func (t *Transport) Heartbeat(ctx context.Context) error {
	p := t.LocalPeer()
	b, _ := json.Marshal(p)
	return t.client.HSet(ctx, t.prefix+"devices", p.ID, string(b)).Err()
}
func (t *Transport) Peers(ctx context.Context) ([]Peer, error) {
	raw, e := t.client.HGetAll(ctx, t.prefix+"devices").Result()
	if e != nil {
		return nil, e
	}
	out := []Peer{}
	for id, v := range raw {
		var p Peer
		if len(v) > 8192 || json.Unmarshal([]byte(v), &p) != nil || p.ID != id || !p.Verify() {
			continue
		}
		if t.store != nil {
			pair, e := t.store.Pair(ctx, id)
			p.Paired = e == nil && pair.Ready() && pair.Peer.SignPublic == p.SignPublic && pair.Peer.ExchangePublic == p.ExchangePublic
		}
		out = append(out, p)
	}
	return out, nil
}
func (t *Transport) Resolve(ctx context.Context, to string) (string, error) {
	if len(to) == 32 {
		if _, e := hex.DecodeString(to); e == nil {
			return to, nil
		}
	}
	if t.store != nil {
		pairs, e := t.store.Pairs(ctx)
		if e == nil {
			id := ""
			for _, p := range pairs {
				if p.Ready() && p.Peer.Name == to {
					if id != "" {
						return "", errors.New("ambiguous paired device name; use ID")
					}
					id = p.Peer.ID
				}
			}
			if id != "" {
				return id, nil
			}
		}
	}
	ps, e := t.Peers(ctx)
	if e != nil {
		return "", errors.New("cannot resolve device name while Redis is unavailable; use its device ID")
	}
	id := ""
	for _, p := range ps {
		if p.Name == to {
			if id != "" {
				return "", errors.New("ambiguous device name; use device ID")
			}
			id = p.ID
		}
	}
	if id == "" {
		return "", errors.New("device not found; initialize both devices in the same workspace")
	}
	return id, nil
}
