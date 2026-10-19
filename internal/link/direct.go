package link

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const directPath = "/agent-link/v1/deliver"

// A peer that stops consuming a file must not hold the send until the overall
// transfer timeout. Healthy large transfers renew this deadline as they progress.
type directConn struct{ net.Conn }

func (c directConn) Read(p []byte) (int, error) {
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	return c.Conn.Read(p)
}
func (c directConn) Write(p []byte) (int, error) {
	c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.Conn.Write(p)
}

type directRouter struct {
	daemon        *Daemon
	networks      []*net.IPNet
	allowLoopback bool // Test-only loopback network; production uses private interfaces.
	active        chan struct{}
}

func startDirect(ctx context.Context, d *Daemon) func() {
	if d.Config.Routing == "cloud" {
		return func() {}
	}
	addresses, networks := privateNetworks()
	if len(addresses) == 0 {
		return func() {}
	}
	listener, e := net.Listen("tcp4", "0.0.0.0:0")
	if e != nil {
		return func() {} // The existing cloud path stays available.
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	for _, address := range addresses {
		d.Transport.endpoints = append(d.Transport.endpoints, net.JoinHostPort(address, port))
	}
	d.Direct = &directRouter{daemon: d, networks: networks, active: make(chan struct{}, 8)}
	server := &http.Server{Handler: d.Direct, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 30 * time.Minute, WriteTimeout: 30 * time.Minute, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192}
	go server.Serve(listener)
	return func() { server.Close() }
}

func (dr *directRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d := dr.daemon
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if r.Method != http.MethodPost || r.URL.Path != directPath || !dr.allowedIP(net.ParseIP(host)) {
		http.NotFound(w, r)
		return
	}
	if dr.active != nil {
		select {
		case dr.active <- struct{}{}:
			defer func() { <-dr.active }()
		default:
			http.Error(w, "receiver busy", http.StatusTooManyRequests)
			return
		}
	}
	controller := http.NewResponseController(w)
	controller.SetReadDeadline(time.Now().Add(3 * time.Second))
	// File framing overhead is bounded independently of the plaintext limit.
	max := d.Config.MaxFileBytes + (d.Config.MaxFileBytes/chunkSize+2)*64 + 256*1024 + 4
	r.Body = http.MaxBytesReader(w, r.Body, max)
	var length uint32
	if binary.Read(r.Body, binary.BigEndian, &length) != nil || length == 0 || length > 256*1024 {
		http.Error(w, "invalid envelope", http.StatusBadRequest)
		return
	}
	wire := make([]byte, length)
	if _, e := io.ReadFull(r.Body, wire); e != nil {
		http.Error(w, "invalid envelope", http.StatusBadRequest)
		return
	}
	m, e := d.Transport.DecodeEnvelope(wire)
	if e != nil || strings.HasPrefix(m.Kind, "pair_") {
		http.Error(w, "unconfirmed sender or invalid envelope", http.StatusForbidden)
		return
	}
	controller.SetReadDeadline(time.Now().Add(30 * time.Minute))
	if m.File != nil {
		unlock, err := d.fileMu.acquire(r.Context(), m.ID)
		if err != nil {
			http.Error(w, "file request canceled", http.StatusRequestTimeout)
			return
		}
		defer unlock()
	}
	if existing, err := d.Store.Get(r.Context(), m.ID); err == nil {
		before, _ := json.Marshal(existing.Message)
		after, _ := json.Marshal(m)
		if !bytes.Equal(before, after) || existing.Direction != "in" {
			http.Error(w, "conflicting message ID", http.StatusConflict)
			return
		}
	}
	if m.File != nil {
		key, err := d.Transport.PairKey(r.Context(), m.From)
		if err != nil {
			http.Error(w, "invalid file sender", http.StatusForbidden)
			return
		}
		path, err := d.Files.receive(m, key, r.Body)
		if err != nil {
			http.Error(w, "file authentication failed", http.StatusBadRequest)
			return
		}
		if e = d.Store.SetFile(r.Context(), m.ID, path); e != nil {
			http.Error(w, "file persistence failed", http.StatusInternalServerError)
			return
		}
	} else {
		var tail [1]byte
		if n, err := r.Body.Read(tail[:]); n != 0 || err != io.EOF {
			http.Error(w, "unexpected body", http.StatusBadRequest)
			return
		}
	}
	if e = d.acceptIncoming(r.Context(), m, "", nil, "lan"); e != nil {
		http.Error(w, "message persistence failed", http.StatusInternalServerError)
		return
	}
	ack := d.base(m.From, "receipt")
	ack.ReplyTo = m.ID
	payload, e := d.Transport.Encode(r.Context(), ack)
	if e != nil {
		http.Error(w, "acknowledgment failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(payload)
}

func (dr *directRouter) send(ctx context.Context, m Message) error {
	d := dr.daemon
	advertisement, ok := d.Transport.directPeer(ctx, m.To)
	if !ok {
		return errors.New("no confirmed direct endpoint")
	}
	wire, e := d.Transport.Encode(ctx, m)
	if e != nil {
		return e
	}
	var header bytes.Buffer
	binary.Write(&header, binary.BigEndian, uint32(len(wire)))
	header.Write(wire)
	for _, endpoint := range advertisement.Endpoints {
		if !dr.allowedEndpoint(endpoint) {
			continue
		}
		var staged *os.File
		var body io.Reader = bytes.NewReader(header.Bytes())
		length := int64(header.Len())
		if m.File != nil {
			staged, e = os.Open(d.Files.stagedPath(m.ID))
			if e != nil {
				return e
			}
			info, err := staged.Stat()
			if err != nil {
				staged.Close()
				return err
			}
			length += info.Size()
			body = io.MultiReader(body, staged)
		}
		timeout := 2 * time.Second
		if m.File != nil {
			timeout = 15 * time.Minute
		}
		attempt, cancel := context.WithTimeout(ctx, timeout)
		transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := (&net.Dialer{Timeout: 350 * time.Millisecond}).DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			return directConn{conn}, nil
		}, ResponseHeaderTimeout: 2 * time.Second, DisableKeepAlives: true, MaxResponseHeaderBytes: 8192}
		client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		req, err := http.NewRequestWithContext(attempt, http.MethodPost, "http://"+endpoint+directPath, body)
		if err == nil {
			req.ContentLength = length
			req.Header.Set("Content-Type", "application/octet-stream")
			res, sendErr := client.Do(req)
			err = sendErr
			if err == nil {
				payload, readErr := io.ReadAll(io.LimitReader(res.Body, 256*1024+1))
				res.Body.Close()
				if res.StatusCode != http.StatusOK || readErr != nil {
					err = errors.New("direct delivery not acknowledged")
				} else {
					ack, decodeErr := d.Transport.DecodeEnvelope(payload)
					if decodeErr != nil || ack.Kind != "receipt" || ack.From != m.To || ack.ReplyTo != m.ID {
						err = errors.New("invalid direct acknowledgment")
					} else {
						err = d.Store.Accept(attempt, &ack, "", nil)
					}
				}
			}
		}
		transport.CloseIdleConnections()
		cancel()
		if staged != nil {
			staged.Close()
		}
		if err == nil {
			return nil
		}
	}
	return errors.New("direct endpoint unavailable")
}
