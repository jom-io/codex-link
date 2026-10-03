package link

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"
)

const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef"

func TestEnvelopeAuthentication(t *testing.T) {
	c, _ := NewCodec("one", testKey)
	v, e := c.Seal([]byte("private payload"))
	if e != nil {
		t.Fatal(e)
	}
	b, e := c.Open(v)
	if e != nil || string(b) != "private payload" {
		t.Fatal(e)
	}
	wrong, _ := NewCodec("two", testKey)
	if _, e = wrong.Open(v); e == nil {
		t.Fatal("cross-workspace envelope accepted")
	}
	wrong, _ = NewCodec("one", testKey+"x")
	if _, e = wrong.Open(v); e == nil {
		t.Fatal("wrong key accepted")
	}
	if _, e = c.Open(v[:len(v)-5]); e == nil {
		t.Fatal("truncated envelope accepted")
	}
}
func TestFileRoundTripAndCorruption(t *testing.T) {
	for _, size := range []int{0, 1, chunkSize, chunkSize + 11, chunkSize * 3} {
		t.Run(string(rune('a'+size%26)), func(t *testing.T) {
			plain := bytes.Repeat([]byte{'x'}, size)
			var enc bytes.Buffer
			n, hash, e := encryptFile(&enc, bytes.NewReader(plain), testKey, "obj", int64(size+1))
			if e != nil || n != int64(size) {
				t.Fatal(e, n)
			}
			expected := sha256.Sum256(plain)
			if hash != hex.EncodeToString(expected[:]) {
				t.Fatal("wrong hash")
			}
			raw := append([]byte(nil), enc.Bytes()...)
			var out bytes.Buffer
			n, h, e := decryptFile(&out, bytes.NewReader(raw), testKey, "obj", int64(size+1))
			if e != nil || n != int64(size) || h != hash || !bytes.Equal(out.Bytes(), plain) {
				t.Fatal("round trip failed", e)
			}
			for _, bad := range [][]byte{raw[:len(raw)-1], append(append([]byte(nil), raw...), 0)} {
				if _, _, e = decryptFile(io.Discard, bytes.NewReader(bad), testKey, "obj", int64(size+1)); e == nil {
					t.Fatal("bad file accepted")
				}
			}
			raw[len(raw)-1] ^= 1
			if _, _, e = decryptFile(io.Discard, bytes.NewReader(raw), testKey, "obj", int64(size+1)); e == nil {
				t.Fatal("corruption accepted")
			}
			if _, _, e = decryptFile(io.Discard, bytes.NewReader(enc.Bytes()), testKey, "other", int64(size+1)); e == nil {
				t.Fatal("wrong object accepted")
			}
		})
	}
}
func TestFileLimit(t *testing.T) {
	var b bytes.Buffer
	if _, _, e := encryptFile(&b, bytes.NewReader([]byte("too large")), testKey, "obj", 3); e == nil {
		t.Fatal("upload size limit ignored")
	}
	b.Reset()
	encryptFile(&b, bytes.NewReader([]byte("too large")), testKey, "obj", 100)
	if _, _, e := decryptFile(io.Discard, &b, testKey, "obj", 3); e == nil {
		t.Fatal("download size limit ignored")
	}
}
func BenchmarkFileEncryption(b *testing.B) {
	data := bytes.Repeat([]byte("x"), 1024*1024)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, e := encryptFile(io.Discard, bytes.NewReader(data), testKey, "obj", int64(len(data))); e != nil {
			b.Fatal(e)
		}
	}
}
