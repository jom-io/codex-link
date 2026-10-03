package link

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

func TestOSSUploadDownloadAndTamper(t *testing.T) {
	var mu sync.Mutex
	objects := map[string][]byte{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("unsigned OSS request")
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case "PUT":
			b, _ := io.ReadAll(r.Body)
			objects[r.URL.Path] = b
			w.WriteHeader(200)
		case "GET":
			b, ok := objects[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(b)
		case "DELETE":
			delete(objects, r.URL.Path)
			w.WriteHeader(204)
		}
	}))
	defer server.Close()
	c := testConfig("mac", NewID(), "unused")
	c.OSS.Endpoint = server.URL
	c.OSS.Bucket = "test-bucket"
	c.OSS.Region = "cn-hangzhou"
	s := Secrets{WorkspaceKey: testKey, AccessKeyID: "test-id", AccessKeySecret: "test-secret"}
	client, e := oss.New(server.URL, s.AccessKeyID, s.AccessKeySecret, oss.UseCname(true), oss.HTTPClient(server.Client()), oss.AuthVersion(oss.AuthV4), oss.Region(c.OSS.Region))
	if e != nil {
		t.Fatal(e)
	}
	bucket, e := client.Bucket(c.OSS.Bucket)
	if e != nil {
		t.Fatal(e)
	}
	home := t.TempDir()
	h := sha256.Sum256([]byte(c.Workspace))
	f := &Files{bucket, c, s, home, "codex-link/" + hex.EncodeToString(h[:8]) + "/"}
	source := filepath.Join(home, "source.txt")
	os.WriteFile(source, []byte("private file payload"), 0600)
	ref, e := f.Upload(context.Background(), source, c.DeviceID)
	if e != nil {
		t.Fatal(e)
	}
	mu.Lock()
	for _, b := range objects {
		if string(b) == "private file payload" {
			t.Fatal("plaintext uploaded")
		}
	}
	mu.Unlock()
	m := Message{ID: NewID(), File: ref}
	dest, e := f.Download(context.Background(), m)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(dest)
	if string(b) != "private file payload" {
		t.Fatal("file mismatch")
	}
	os.Remove(dest)
	mu.Lock()
	for k, b := range objects {
		b[len(b)-1] ^= 1
		objects[k] = b
	}
	mu.Unlock()
	if _, e = f.Download(context.Background(), m); e == nil {
		t.Fatal("tampered OSS file accepted")
	}
	if _, e = os.Stat(dest); !os.IsNotExist(e) {
		t.Fatal("partial file published")
	}
	ref.Object = "../../outside"
	if _, e = f.Download(context.Background(), m); e == nil {
		t.Fatal("foreign object accepted")
	}
}
