package link

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Identity struct {
	signing  ed25519.PrivateKey
	exchange *ecdh.PrivateKey
}

func EnsureIdentity(s *Secrets) (*Identity, error) {
	if s.SigningPrivate == "" && s.ExchangePrivate == "" {
		_, priv, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return nil, e
		}
		x, e := ecdh.X25519().GenerateKey(rand.Reader)
		if e != nil {
			return nil, e
		}
		s.SigningPrivate = base64.RawStdEncoding.EncodeToString(priv)
		s.ExchangePrivate = base64.RawStdEncoding.EncodeToString(x.Bytes())
	}
	b, e := base64.RawStdEncoding.DecodeString(s.SigningPrivate)
	if e != nil || len(b) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid device signing identity")
	}
	x, e := base64.RawStdEncoding.DecodeString(s.ExchangePrivate)
	if e != nil {
		return nil, errors.New("invalid device exchange identity")
	}
	priv, e := ecdh.X25519().NewPrivateKey(x)
	if e != nil {
		return nil, e
	}
	return &Identity{ed25519.PrivateKey(b), priv}, nil
}
func (i *Identity) ID() string { return identityID(i.SignPublic()) }
func identityID(public string) string {
	b, e := base64.RawStdEncoding.DecodeString(public)
	if e != nil || len(b) != ed25519.PublicKeySize {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:16])
}
func (i *Identity) SignPublic() string {
	return base64.RawStdEncoding.EncodeToString(i.signing.Public().(ed25519.PublicKey))
}
func (i *Identity) ExchangePublic() string {
	return base64.RawStdEncoding.EncodeToString(i.exchange.PublicKey().Bytes())
}
func (i *Identity) Sign(b []byte) string {
	return base64.RawStdEncoding.EncodeToString(ed25519.Sign(i.signing, b))
}
func verifySignature(public string, b []byte, signature string) bool {
	p, e := base64.RawStdEncoding.DecodeString(public)
	if e != nil || len(p) != ed25519.PublicKeySize {
		return false
	}
	s, e := base64.RawStdEncoding.DecodeString(signature)
	return e == nil && ed25519.Verify(ed25519.PublicKey(p), b, s)
}
func (i *Identity) PairKey(workspace string, p Peer) (string, error) {
	public, e := base64.RawStdEncoding.DecodeString(p.ExchangePublic)
	if e != nil {
		return "", errors.New("invalid peer exchange key")
	}
	key, e := ecdh.X25519().NewPublicKey(public)
	if e != nil {
		return "", e
	}
	shared, e := i.exchange.ECDH(key)
	if e != nil {
		return "", e
	}
	ids := []string{i.ID(), p.ID}
	sort.Strings(ids)
	salt := sha256.Sum256([]byte("codex-link/v2/" + workspace))
	extract := hmac.New(sha256.New, salt[:])
	extract.Write(shared)
	prk := extract.Sum(nil)
	expand := hmac.New(sha256.New, prk)
	expand.Write([]byte("pair/" + strings.Join(ids, "/") + "/" + pinnedContext(i, p)))
	expand.Write([]byte{1})
	return base64.RawStdEncoding.EncodeToString(expand.Sum(nil)), nil
}
func pinnedContext(i *Identity, p Peer) string {
	keys := []string{i.SignPublic() + ":" + i.ExchangePublic(), p.SignPublic + ":" + p.ExchangePublic}
	sort.Strings(keys)
	return strings.Join(keys, "/")
}
func pairCode(local Peer, remote Peer, nonce string) string {
	keys := []string{local.SignPublic + ":" + local.ExchangePublic, remote.SignPublic + ":" + remote.ExchangePublic}
	sort.Strings(keys)
	h := sha256.Sum256([]byte("codex-link/pair/" + nonce + "/" + strings.Join(keys, "/")))
	n := uint32(h[0])<<24 | uint32(h[1])<<16 | uint32(h[2])<<8 | uint32(h[3])
	return fmt.Sprintf("%06d", n%1000000)
}

type Wire struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Public    string `json:"public"`
	Payload   string `json:"payload"`
	Plain     bool   `json:"plain,omitempty"`
	Signature string `json:"signature"`
}

func (w Wire) signedBytes() []byte { w.Signature = ""; b, _ := json.Marshal(w); return b }
func (p Peer) signedBytes() []byte {
	p.Signature = ""
	p.Paired = false
	b, _ := json.Marshal(p)
	return b
}
func (p Peer) Verify() bool {
	return p.ID == identityID(p.SignPublic) && ValidLabel(p.Name) && p.Protocol == ProtocolVersion && verifySignature(p.SignPublic, p.signedBytes(), p.Signature)
}
