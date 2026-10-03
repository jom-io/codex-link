package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type Daemon struct {
	Config    Config
	Home      string
	Store     *Store
	Transport *Transport
	Files     *Files
	mu        sync.Mutex
	changed   chan struct{}
	fileMu    sync.Mutex
	statusMu  sync.Mutex
	connected bool
}

func (d *Daemon) notify() {
	d.mu.Lock()
	close(d.changed)
	d.changed = make(chan struct{})
	d.mu.Unlock()
}
func (d *Daemon) changedChan() <-chan struct{} { d.mu.Lock(); defer d.mu.Unlock(); return d.changed }
func (d *Daemon) setConnected(v bool)          { d.statusMu.Lock(); d.connected = v; d.statusMu.Unlock() }
func (d *Daemon) isConnected() bool            { d.statusMu.Lock(); defer d.statusMu.Unlock(); return d.connected }
func (d *Daemon) base(to, kind string) Message {
	return Message{ID: NewID(), Protocol: ProtocolVersion, From: d.Config.DeviceID, FromName: d.Config.Name, To: to, Kind: kind, CreatedAt: time.Now().UTC()}
}
func (d *Daemon) receive(ctx context.Context) {
	for ctx.Err() == nil {
		cursor, e := d.Store.Cursor(ctx)
		if e != nil {
			log.Print("local cursor read failed")
			return
		}
		raw, e := d.Transport.Read(ctx, cursor)
		if e != nil {
			d.setConnected(false)
			log.Print("Redis receive unavailable; retrying")
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		d.setConnected(true)
		for _, r := range raw {
			m, e := d.Transport.Decode(r)
			if e != nil {
				log.Print("rejected invalid or unauthenticated envelope")
				if e = d.Store.Accept(ctx, nil, r.ID, nil); e != nil {
					log.Print("cursor persistence failed")
					break
				}
				continue
			}
			var receipt *Message
			if m.Kind != "receipt" {
				v := d.base(m.From, "receipt")
				v.ReplyTo = m.ID
				receipt = &v
			}
			if e = d.Store.Accept(ctx, &m, r.ID, receipt); e != nil {
				log.Print("message persistence failed; will retry")
				break
			}
			d.notify()
		}
	}
}
func (d *Daemon) send(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	heart := time.NewTicker(30 * time.Second)
	defer heart.Stop()
	heartbeat := func() {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		e := d.Transport.Heartbeat(c)
		d.setConnected(e == nil)
	}
	heartbeat()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heart.C:
			heartbeat()
		case <-tick.C:
			rs, e := d.Store.Pending(ctx)
			if e != nil {
				log.Print("outbox read failed")
				continue
			}
			for _, r := range rs {
				c, cancel := context.WithTimeout(ctx, 10*time.Second)
				e = d.Transport.Publish(c, r.Message)
				cancel()
				if e != nil {
					d.setConnected(false)
					break
				}
				d.setConnected(true)
				if e = d.Store.MarkSent(ctx, r.Message.ID); e != nil {
					log.Print("outbox state persistence failed")
					break
				}
			}
		}
	}
}
func RunDaemon(ctx context.Context, home string) error {
	c, e := LoadConfig(home)
	if e != nil {
		return e
	}
	s, e := LoadSecrets(c)
	if e != nil {
		return e
	}
	return RunDaemonWith(ctx, home, c, s)
}
func RunDaemonWith(ctx context.Context, home string, c Config, s Secrets) error {
	c.Defaults()
	if err := c.Validate(); err != nil {
		return err
	}
	if e := os.MkdirAll(home, 0700); e != nil {
		return e
	}
	lock, e := os.OpenFile(filepath.Join(home, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return errors.New("daemon already running")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	socket := filepath.Join(home, "run.sock")
	if len(socket) > 100 {
		return errors.New("data directory too long for macOS Unix socket; use a shorter CODEX_LINK_HOME")
	}
	os.Remove(socket)
	listener, e := net.Listen("unix", socket)
	if e != nil {
		return e
	}
	defer listener.Close()
	defer os.Remove(socket)
	if e = os.Chmod(socket, 0600); e != nil {
		return e
	}
	store, e := OpenStore(home)
	if e != nil {
		return e
	}
	defer store.Close()
	transport, e := NewTransport(c, s)
	if e != nil {
		return e
	}
	defer transport.Close()
	files, _ := NewFiles(c, s, home)
	d := &Daemon{Config: c, Home: home, Store: store, Transport: transport, Files: files, changed: make(chan struct{})}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); d.receive(runCtx) }()
	go func() { defer wg.Done(); d.send(runCtx) }()
	server := &http.Server{Handler: d.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
	case e = <-done:
		if !errors.Is(e, http.ErrServerClosed) {
			cancel()
			transport.Close()
			wg.Wait()
			return e
		}
	}
	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	server.Shutdown(shutdownCtx)
	transport.Close()
	wg.Wait()
	return nil
}

type APIRequest struct {
	To           string `json:"to,omitempty"`
	Text         string `json:"text,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Session      string `json:"session,omitempty"`
	ToSession    string `json:"to_session,omitempty"`
	Conversation string `json:"conversation,omitempty"`
	ReplyTo      string `json:"reply_to,omitempty"`
	ID           string `json:"id,omitempty"`
	Path         string `json:"path,omitempty"`
	After        int64  `json:"after"`
	Limit        int    `json:"limit,omitempty"`
	History      bool   `json:"history,omitempty"`
	Timeout      int    `json:"timeout,omitempty"`
	Lease        int    `json:"lease,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func (d *Daemon) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeJSON(w, 405, map[string]string{"error": "POST required"})
			return
		}
		var a APIRequest
		r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
		if e := json.NewDecoder(r.Body).Decode(&a); e != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid request"})
			return
		}
		v, e := d.dispatch(r.Context(), r.URL.Path, a)
		if e != nil {
			writeJSON(w, 400, map[string]string{"error": e.Error()})
			return
		}
		writeJSON(w, 200, v)
	})
	return mux
}
func (d *Daemon) dispatch(ctx context.Context, path string, a APIRequest) (any, error) {
	switch path {
	case "/v1/status":
		n, e := d.Store.MaxSeq(ctx)
		return map[string]any{"version": Version, "protocol": ProtocolVersion, "device_id": d.Config.DeviceID, "name": d.Config.Name, "workspace": d.Config.Workspace, "redis_connected": d.isConnected(), "oss_configured": d.Files != nil, "last_seq": n}, e
	case "/v1/peers":
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return d.Transport.Peers(c)
	case "/v1/send", "/v1/file/send":
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		to, e := d.Transport.Resolve(c, a.To)
		cancel()
		if e != nil {
			return nil, e
		}
		kind := a.Kind
		if kind == "" {
			kind = "text"
		}
		if path == "/v1/file/send" {
			kind = "file"
		}
		if kind == "receipt" {
			return nil, errors.New("receipts are internal")
		}
		m := d.base(to, kind)
		m.Text = a.Text
		m.Session = a.Session
		m.ToSession = a.ToSession
		m.Conversation = a.Conversation
		m.ReplyTo = a.ReplyTo
		if kind == "file" {
			if d.Files == nil {
				return nil, errors.New("OSS is not configured")
			}
			d.fileMu.Lock()
			m.File, e = d.Files.Upload(ctx, a.Path, to)
			d.fileMu.Unlock()
			if e != nil {
				return nil, e
			}
		}
		if e = d.Store.Enqueue(ctx, m); e != nil {
			return nil, e
		}
		d.notify()
		return map[string]any{"id": m.ID, "status": "pending", "message": m}, nil
	case "/v1/inbox", "/v1/history":
		return d.Store.List(ctx, a.After, a.Limit, path == "/v1/history", a.Session, a.Conversation)
	case "/v1/wait":
		if a.Timeout < 1 || a.Timeout > 60 {
			return nil, errors.New("timeout must be 1–60 seconds")
		}
		if a.After < 0 {
			n, e := d.Store.MaxSeq(ctx)
			if e != nil {
				return nil, e
			}
			a.After = n
		}
		timer := time.NewTimer(time.Duration(a.Timeout) * time.Second)
		defer timer.Stop()
		for {
			ch := d.changedChan()
			rs, e := d.Store.List(ctx, a.After, a.Limit, false, a.Session, a.Conversation)
			if e != nil || len(rs) > 0 {
				return rs, e
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
				return []Record{}, nil
			case <-ch:
			}
		}
	case "/v1/get":
		return d.Store.Get(ctx, a.ID)
	case "/v1/file/fetch":
		if d.Files == nil {
			return nil, errors.New("OSS is not configured")
		}
		r, e := d.Store.Get(ctx, a.ID)
		if e != nil {
			return nil, e
		}
		if r.Direction != "in" {
			return nil, errors.New("only received files can be fetched")
		}
		d.fileMu.Lock()
		p, e := d.Files.Download(ctx, r.Message)
		d.fileMu.Unlock()
		if e != nil {
			return nil, e
		}
		if e = d.Store.SetFile(ctx, a.ID, p); e != nil {
			return nil, e
		}
		return map[string]string{"id": a.ID, "path": p}, nil
	case "/v1/task/claim":
		if a.Lease == 0 {
			a.Lease = 900
		}
		if e := d.Store.Claim(ctx, a.ID, a.Session, a.Lease); e != nil {
			return nil, e
		}
		return d.Store.Get(ctx, a.ID)
	case "/v1/task/complete":
		r, e := d.Store.Get(ctx, a.ID)
		if e != nil {
			return nil, e
		}
		m := d.base(r.Message.From, "result")
		m.Text = a.Text
		m.Session = a.Session
		m.ToSession = r.Message.Session
		m.Conversation = r.Message.Conversation
		m.ReplyTo = a.ID
		if e = d.Store.Complete(ctx, a.ID, a.Session, a.Text, m); e != nil {
			return nil, e
		}
		d.notify()
		return map[string]string{"id": a.ID, "status": "completed", "reply_id": m.ID}, nil
	default:
		return nil, fmt.Errorf("unknown operation")
	}
}
func Call(ctx context.Context, home, op string, a APIRequest) (json.RawMessage, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(home, "run.sock"))
	}}
	defer transport.CloseIdleConnections()
	b, _ := json.Marshal(a)
	req, e := http.NewRequestWithContext(ctx, "POST", "http://localhost/v1/"+op, bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: transport}
	res, e := client.Do(req)
	if e != nil {
		return nil, errors.New("daemon unavailable; run codex-link daemon start (or daemon run)")
	}
	defer res.Body.Close()
	var out json.RawMessage
	if e = json.NewDecoder(res.Body).Decode(&out); e != nil {
		return nil, e
	}
	if res.StatusCode != 200 {
		var v struct {
			Error string `json:"error"`
		}
		json.Unmarshal(out, &v)
		return nil, errors.New(v.Error)
	}
	return out, nil
}
