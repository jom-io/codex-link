package link

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

const chunkSize = 64 * 1024
const fileMagic = "CLFILE01"

func fileAEAD(key string) (cipher.AEAD, error) {
	k := sha256.Sum256([]byte("codex-link/v1/file/" + key))
	b, e := aes.NewCipher(k[:])
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(b)
}

// Each frame authenticates its index and object key. A final empty frame detects truncation.
func frameAAD(object string, index uint64) []byte {
	return []byte(fmt.Sprintf("codex-link/file/v1/%s/%d", object, index))
}
func encryptFile(dst io.Writer, src io.Reader, key, object string, max int64) (int64, string, error) {
	a, e := fileAEAD(key)
	if e != nil {
		return 0, "", e
	}
	if _, e = io.WriteString(dst, fileMagic); e != nil {
		return 0, "", e
	}
	h := sha256.New()
	buf := make([]byte, chunkSize)
	var total int64
	var index uint64
	for {
		n, readErr := io.ReadFull(src, buf)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return 0, "", readErr
		}
		total += int64(n)
		if total > max {
			return 0, "", errors.New("file exceeds configured size limit")
		}
		h.Write(buf[:n])
		nonce := make([]byte, a.NonceSize())
		if _, e = rand.Read(nonce); e != nil {
			return 0, "", e
		}
		frame := a.Seal(nonce, nonce, buf[:n], frameAAD(object, index))
		if e = binary.Write(dst, binary.BigEndian, uint32(len(frame))); e != nil {
			return 0, "", e
		}
		if _, e = dst.Write(frame); e != nil {
			return 0, "", e
		}
		index++
		if n == 0 {
			break
		}
		if readErr != nil {
			src = strings.NewReader("")
		}
	}
	return total, hex.EncodeToString(h.Sum(nil)), nil
}
func decryptFile(dst io.Writer, src io.Reader, key, object string, max int64) (int64, string, error) {
	a, e := fileAEAD(key)
	if e != nil {
		return 0, "", e
	}
	magic := make([]byte, len(fileMagic))
	if _, e = io.ReadFull(src, magic); e != nil || string(magic) != fileMagic {
		return 0, "", errors.New("invalid encrypted file")
	}
	h := sha256.New()
	var total int64
	var index uint64
	for {
		var size uint32
		if e = binary.Read(src, binary.BigEndian, &size); e != nil {
			return 0, "", errors.New("truncated encrypted file")
		}
		if size < uint32(a.NonceSize()+a.Overhead()) || size > chunkSize+uint32(a.NonceSize()+a.Overhead()) {
			return 0, "", errors.New("invalid frame size")
		}
		b := make([]byte, size)
		if _, e = io.ReadFull(src, b); e != nil {
			return 0, "", e
		}
		plain, e := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], frameAAD(object, index))
		if e != nil {
			return 0, "", errors.New("file authentication failed")
		}
		index++
		if len(plain) == 0 {
			var tail [1]byte
			n, e := src.Read(tail[:])
			if n != 0 || e != io.EOF {
				return 0, "", errors.New("unexpected trailing file data")
			}
			break
		}
		total += int64(len(plain))
		if total > max {
			return 0, "", errors.New("file exceeds configured size limit")
		}
		h.Write(plain)
		if _, e = dst.Write(plain); e != nil {
			return 0, "", e
		}
	}
	return total, hex.EncodeToString(h.Sum(nil)), nil
}

type Files struct {
	bucket  *oss.Bucket
	config  Config
	secrets Secrets
	home    string
	root    string
}

func NewFiles(c Config, s Secrets, home string) (*Files, error) {
	if c.OSS.Endpoint == "" || c.OSS.Bucket == "" || s.AccessKeyID == "" || s.AccessKeySecret == "" {
		return nil, errors.New("OSS is not configured; run configure")
	}
	opts := []oss.ClientOption{oss.Timeout(10, 120), oss.AuthVersion(oss.AuthV4), oss.Region(c.OSS.Region)}
	if s.SecurityToken != "" {
		opts = append(opts, oss.SecurityToken(s.SecurityToken))
	}
	client, e := oss.New(c.OSS.Endpoint, s.AccessKeyID, s.AccessKeySecret, opts...)
	if e != nil {
		return nil, e
	}
	bucket, e := client.Bucket(c.OSS.Bucket)
	if e != nil {
		return nil, e
	}
	h := sha256.Sum256([]byte(c.Workspace))
	root := strings.Trim(c.OSS.Prefix, "/") + "/" + hex.EncodeToString(h[:8]) + "/"
	return &Files{bucket, c, s, home, root}, nil
}
func (f *Files) Upload(ctx context.Context, path, to, key string) (*FileRef, error) {
	if !ValidLabel(to) {
		return nil, errors.New("invalid recipient")
	}
	src, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer src.Close()
	stat, e := src.Stat()
	if e != nil {
		return nil, e
	}
	if !stat.Mode().IsRegular() {
		return nil, errors.New("send a regular file; archive directories explicitly first")
	}
	if stat.Size() > f.config.MaxFileBytes {
		return nil, errors.New("file exceeds configured size limit")
	}
	tmp, e := os.CreateTemp(f.home, "upload-*.encrypted")
	if e != nil {
		return nil, e
	}
	defer os.Remove(tmp.Name())
	object := f.root + to + "/" + NewID()
	n, hash, e := encryptFile(tmp, src, key, object, f.config.MaxFileBytes)
	ce := tmp.Close()
	if e != nil {
		return nil, e
	}
	if ce != nil {
		return nil, ce
	}
	if e = f.bucket.PutObjectFromFile(object, tmp.Name(), oss.WithContext(ctx), oss.ContentType("application/octet-stream")); e != nil {
		return nil, errors.New("OSS upload failed; check credentials, bucket permissions and connectivity")
	}
	return &FileRef{object, filepath.Base(path), n, hash}, nil
}
func (f *Files) Download(ctx context.Context, m Message, key string) (string, error) {
	if m.File == nil {
		return "", errors.New("message has no file")
	}
	r := m.File
	prefix := f.root + f.config.DeviceID + "/"
	suffix := strings.TrimPrefix(r.Object, prefix)
	if !strings.HasPrefix(r.Object, prefix) || !ValidLabel(suffix) || r.Size > f.config.MaxFileBytes || r.Size < 0 {
		return "", errors.New("invalid file object or size")
	}
	if !ValidLabel(m.ID) {
		return "", errors.New("invalid message ID")
	}
	name := filepath.Base(r.Name)
	if name == "." || name == ".." || name == "/" || strings.ContainsAny(name, "\\\x00") {
		return "", errors.New("invalid file name")
	}
	dir := filepath.Join(f.home, "files", m.ID)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	path := filepath.Join(dir, name)
	if existing, e := os.Open(path); e == nil {
		h := sha256.New()
		n, e := io.Copy(h, io.LimitReader(existing, f.config.MaxFileBytes+1))
		existing.Close()
		if e == nil && n == r.Size && hex.EncodeToString(h.Sum(nil)) == r.SHA256 {
			return path, nil
		}
	}
	body, e := f.bucket.GetObject(r.Object, oss.WithContext(ctx))
	if e != nil {
		return "", errors.New("OSS download failed; file may have expired or credentials need renewal")
	}
	defer body.Close()
	tmp, e := os.CreateTemp(dir, ".download-*")
	if e != nil {
		return "", e
	}
	defer os.Remove(tmp.Name())
	n, hash, e := decryptFile(tmp, body, key, r.Object, f.config.MaxFileBytes)
	ce := tmp.Close()
	if e != nil {
		return "", e
	}
	if ce != nil {
		return "", ce
	}
	if n != r.Size || hash != r.SHA256 {
		return "", errors.New("file size or SHA-256 mismatch")
	}
	if e = os.Rename(tmp.Name(), path); e != nil {
		return "", e
	}
	return path, nil
}

// Check verifies the configured bucket with an encrypted round trip and removes the probe.
func (f *Files) Check(ctx context.Context) error {
	tmp, e := os.CreateTemp(f.home, "probe-*")
	if e != nil {
		return e
	}
	defer os.Remove(tmp.Name())
	payload := []byte("codex-link encrypted OSS connectivity probe")
	if _, e = tmp.Write(payload); e != nil {
		tmp.Close()
		return e
	}
	tmp.Close()
	key := NewID() + NewID()
	ref, e := f.Upload(ctx, tmp.Name(), f.config.DeviceID, key)
	if e != nil {
		return e
	}
	defer f.bucket.DeleteObject(ref.Object, oss.WithContext(ctx))
	m := Message{ID: NewID(), File: ref}
	p, e := f.Download(ctx, m, key)
	if e != nil {
		return e
	}
	defer os.RemoveAll(filepath.Dir(p))
	b, e := os.ReadFile(p)
	if e != nil {
		return e
	}
	if string(b) != string(payload) {
		return errors.New("OSS probe mismatch")
	}
	return nil
}
