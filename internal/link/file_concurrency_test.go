package link

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

type pausedFileBody struct {
	io.Reader
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *pausedFileBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return b.Reader.Read(p)
}

func TestSlowDirectFileDoesNotBlockLocalFile(t *testing.T) {
	da, db := pairedRoutingDaemons(t, "127.0.0.1:1")
	ctx := context.Background()
	remoteSource := filepath.Join(da.Home, "remote.txt")
	if err := os.WriteFile(remoteSource, []byte("slow remote file"), 0600); err != nil {
		t.Fatal(err)
	}
	m := queued(t, da, "file/send", APIRequest{To: db.Config.DeviceID, Path: remoteSource})
	wire, err := da.Transport.Encode(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := os.ReadFile(da.Files.stagedPath(m.ID))
	if err != nil {
		t.Fatal(err)
	}
	var header bytes.Buffer
	binary.Write(&header, binary.BigEndian, uint32(len(wire)))
	header.Write(wire)
	body := &pausedFileBody{Reader: bytes.NewReader(encrypted), entered: make(chan struct{}), release: make(chan struct{})}
	req := httptest.NewRequest(http.MethodPost, directPath, io.MultiReader(&header, body))
	req.RemoteAddr = "127.0.0.1:45678"
	response := httptest.NewRecorder()
	directDone := make(chan struct{})
	go func() { db.Direct.ServeHTTP(response, req); close(directDone) }()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(body.release) }) }
	defer func() { release(); <-directDone }()
	select {
	case <-body.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("receiver did not begin reading the remote file")
	}
	localSource := filepath.Join(db.Home, "local.txt")
	payload := []byte("local traffic must not wait for a different remote file")
	if err = os.WriteFile(localSource, payload, 0600); err != nil {
		t.Fatal(err)
	}
	localDone := make(chan error, 1)
	go func() {
		out, e := db.dispatch(ctx, "/v1/file/send", APIRequest{To: "self", Path: localSource})
		if e != nil {
			localDone <- e
			return
		}
		id := out.(map[string]any)["id"].(string)
		r, e := db.Store.Get(ctx, id)
		if e == nil {
			e = db.deliverOutgoing(ctx, r.Message)
		}
		if e == nil {
			var fetched any
			fetched, e = db.dispatch(ctx, "/v1/file/fetch", APIRequest{ID: id})
			if e == nil {
				var got []byte
				got, e = os.ReadFile(fetched.(map[string]string)["path"])
				if e == nil && !bytes.Equal(got, payload) {
					e = io.ErrUnexpectedEOF
				}
			}
		}
		if e == nil {
			r, e = db.Store.Get(ctx, id)
			if e == nil && r.Route != "local" {
				e = io.ErrNoProgress
			}
		}
		localDone <- e
	}()
	select {
	case err = <-localDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		release()
		<-localDone
		t.Fatal("unrelated slow remote file blocked local send/delivery/fetch")
	}
	release()
	<-directDone
	if response.Code != http.StatusOK {
		t.Fatal("remote transfer did not finish after resuming", response.Code)
	}
}

func TestFileLockCancellationAndRelease(t *testing.T) {
	var locks fileLocks
	unlock, err := locks.acquire(context.Background(), "same-file")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	waiter := make(chan error, 1)
	go func() {
		release, e := locks.acquire(ctx, "same-file")
		if e == nil {
			release()
		}
		waiter <- e
	}()
	deadline := time.Now().Add(time.Second)
	for {
		locks.mu.Lock()
		waiting := locks.entries["same-file"].users == 2
		locks.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			unlock()
			t.Fatal("same-file waiter did not enter the lock queue")
		}
		runtime.Gosched()
	}
	other, err := locks.acquire(context.Background(), "different-file")
	if err != nil {
		t.Fatal(err)
	}
	other()
	cancel()
	select {
	case err = <-waiter:
		if err != context.Canceled {
			t.Fatal("same-file waiter was not canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled same-file waiter remained blocked")
	}
	unlock()
	if len(locks.entries) != 0 {
		t.Fatal("inactive file locks were retained")
	}
	release, err := locks.acquire(context.Background(), "same-file")
	if err != nil {
		t.Fatal(err)
	}
	release()
}
