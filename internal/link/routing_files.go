package link

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

func newLocalFiles(c Config, home string) *Files {
	h := sha256.Sum256([]byte(c.Workspace))
	return &Files{config: c, home: home, root: strings.Trim(c.OSS.Prefix, "/") + "/" + hex.EncodeToString(h[:8]) + "/"}
}

func (f *Files) stagedPath(id string) string {
	return filepath.Join(f.home, "outbox-files", id+".encrypted")
}

func validFileID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, e := hex.DecodeString(id)
	return e == nil
}

// Snapshot once, before enqueueing. Retry and cloud fallback use these exact
// encrypted bytes even if the user later edits/removes the source file.
func (f *Files) stage(path, to, id, key string) (*FileRef, error) {
	if !ValidLabel(to) || !validFileID(id) {
		return nil, errors.New("invalid file routing")
	}
	src, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer src.Close()
	info, e := src.Stat()
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Size() > f.config.MaxFileBytes {
		return nil, errors.New("send a regular file within the configured size limit")
	}
	dir := filepath.Join(f.home, "outbox-files")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	tmp, e := os.CreateTemp(dir, ".stage-*")
	if e != nil {
		return nil, e
	}
	defer os.Remove(tmp.Name())
	object := f.root + to + "/" + NewID()
	n, hash, e := encryptFile(tmp, src, key, object, f.config.MaxFileBytes)
	closeErr := tmp.Close()
	if e != nil {
		return nil, e
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if e = os.Rename(tmp.Name(), f.stagedPath(id)); e != nil {
		return nil, e
	}
	return &FileRef{Object: object, Name: filepath.Base(path), Size: n, SHA256: hash}, nil
}

func (f *Files) uploadStaged(ctx context.Context, m Message) error {
	if f.bucket == nil {
		return errors.New("direct delivery unavailable and OSS is not configured")
	}
	if e := f.bucket.PutObjectFromFile(m.File.Object, f.stagedPath(m.ID), oss.WithContext(ctx), oss.ContentType("application/octet-stream")); e != nil {
		return errors.New("OSS fallback upload failed")
	}
	return nil
}

func (f *Files) cachePath(m Message) (string, error) {
	if m.File == nil || !validFileID(m.ID) {
		return "", errors.New("invalid file message")
	}
	r := m.File
	prefix := f.root + f.config.DeviceID + "/"
	if !strings.HasPrefix(r.Object, prefix) || !validFileID(strings.TrimPrefix(r.Object, prefix)) || r.Size < 0 || r.Size > f.config.MaxFileBytes {
		return "", errors.New("invalid file object or size")
	}
	name := filepath.Base(r.Name)
	if name == "." || name == ".." || name == "/" || strings.ContainsAny(name, "\\\x00") {
		return "", errors.New("invalid file name")
	}
	return filepath.Join(f.home, "files", m.ID, name), nil
}

func (f *Files) cached(m Message) (string, bool) {
	path, e := f.cachePath(m)
	if e != nil {
		return "", false
	}
	src, e := os.Open(path)
	if e != nil {
		return "", false
	}
	defer src.Close()
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(src, f.config.MaxFileBytes+1))
	return path, e == nil && n == m.File.Size && hex.EncodeToString(h.Sum(nil)) == m.File.SHA256
}

// Both LAN pushes and OSS downloads publish only a fully authenticated file.
func (f *Files) receive(m Message, key string, src io.Reader) (string, error) {
	path, e := f.cachePath(m)
	if e != nil {
		return "", e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return "", e
	}
	tmp, e := os.CreateTemp(filepath.Dir(path), ".download-*")
	if e != nil {
		return "", e
	}
	defer os.Remove(tmp.Name())
	n, hash, e := decryptFile(tmp, src, key, m.File.Object, f.config.MaxFileBytes)
	closeErr := tmp.Close()
	if e != nil {
		return "", e
	}
	if closeErr != nil {
		return "", closeErr
	}
	if n != m.File.Size || hash != m.File.SHA256 {
		return "", errors.New("file size or SHA-256 mismatch")
	}
	if e = os.Rename(tmp.Name(), path); e != nil {
		return "", e
	}
	return path, nil
}

func (f *Files) Download(ctx context.Context, m Message, key string) (string, error) {
	if _, e := f.cachePath(m); e != nil {
		return "", e
	}
	if path, ok := f.cached(m); ok {
		return path, nil
	}
	if f.bucket == nil {
		return "", errors.New("file is not cached locally and OSS is not configured")
	}
	body, e := f.bucket.GetObject(m.File.Object, oss.WithContext(ctx))
	if e != nil {
		return "", errors.New("OSS download failed; file may have expired or credentials need renewal")
	}
	defer body.Close()
	return f.receive(m, key, body)
}
