package link

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

// Workspace-scoped authenticated encryption. Only paired devices know the key.
type Codec struct {
	aead cipher.AEAD
	aad  []byte
}

func NewCodec(workspace, key string) (*Codec, error) {
	if len(key) < 32 {
		return nil, errors.New("workspace key must have at least 32 characters")
	}
	k := sha256.Sum256([]byte("codex-link/v1/key/" + key))
	b, e := aes.NewCipher(k[:])
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	return &Codec{a, []byte("codex-link/v1/" + workspace)}, nil
}
func (c *Codec) Seal(b []byte) (string, error) {
	n := make([]byte, c.aead.NonceSize())
	if _, e := rand.Read(n); e != nil {
		return "", e
	}
	out := c.aead.Seal(n, n, b, c.aad)
	return base64.RawStdEncoding.EncodeToString(out), nil
}
func (c *Codec) Open(s string) ([]byte, error) {
	b, e := base64.RawStdEncoding.DecodeString(s)
	if e != nil {
		return nil, e
	}
	n := c.aead.NonceSize()
	if len(b) < n {
		return nil, errors.New("invalid encrypted envelope")
	}
	return c.aead.Open(nil, b[:n], b[n:], c.aad)
}
