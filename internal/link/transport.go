package link

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

type Transport struct {
	client *redis.Client
	config Config
	codec  *Codec
	prefix string
}

func NewTransport(c Config, s Secrets) (*Transport, error) {
	codec, e := NewCodec(c.Workspace, s.WorkspaceKey)
	if e != nil {
		return nil, e
	}
	o := &redis.Options{Addr: c.Redis.Address, Username: c.Redis.Username, Password: s.RedisPassword, DB: c.Redis.DB, DialTimeout: 5 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 5 * time.Second, MaxRetries: 1}
	if c.Redis.TLS {
		o.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	hash := sha256.Sum256([]byte(c.Workspace))
	return &Transport{redis.NewClient(o), c, codec, "cl:v1:{" + hex.EncodeToString(hash[:8]) + "}:"}, nil
}
func (t *Transport) Close() error                   { return t.client.Close() }
func (t *Transport) Ping(ctx context.Context) error { return t.client.Ping(ctx).Err() }
func (t *Transport) Inbox(id string) string         { return t.prefix + "inbox:" + id }

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
	payload, e := t.codec.Seal(b)
	if e != nil {
		return e
	}
	return publishScript.Run(ctx, t.client, []string{t.Inbox(m.To), t.prefix + "sent:" + m.To + ":" + m.ID}, payload, t.config.StreamMaxLen, t.config.StreamTTLHours*3600).Err()
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
	if !ok || len(payload) > 128*1024 {
		return m, errors.New("invalid envelope")
	}
	b, e := t.codec.Open(payload)
	if e != nil {
		return m, e
	}
	if e = json.Unmarshal(b, &m); e != nil {
		return m, e
	}
	if e = m.Validate(); e != nil {
		return m, e
	}
	if m.To != t.config.DeviceID {
		return m, errors.New("wrong recipient")
	}
	return m, nil
}
func (t *Transport) Heartbeat(ctx context.Context) error {
	p := Peer{t.config.DeviceID, t.config.Name, ProtocolVersion, time.Now().UTC()}
	b, _ := json.Marshal(p)
	v, e := t.codec.Seal(b)
	if e != nil {
		return e
	}
	return t.client.HSet(ctx, t.prefix+"devices", p.ID, v).Err()
}
func (t *Transport) Peers(ctx context.Context) ([]Peer, error) {
	raw, e := t.client.HGetAll(ctx, t.prefix+"devices").Result()
	if e != nil {
		return nil, e
	}
	out := []Peer{}
	for id, v := range raw {
		b, e := t.codec.Open(v)
		if e != nil {
			continue
		}
		var p Peer
		if json.Unmarshal(b, &p) != nil || p.ID != id || !ValidLabel(p.ID) || !ValidLabel(p.Name) || p.Protocol != ProtocolVersion {
			continue
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
		return "", errors.New("device not found; initialize both devices in the same workspace with the same key")
	}
	return id, nil
}
