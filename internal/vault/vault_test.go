package vault

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func newTestVault(t *testing.T) *Vault {
	t.Helper()
	key := make([]byte, keyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	v, err := New(key)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return v
}

func TestRoundTrip(t *testing.T) {
	v := newTestVault(t)
	cases := [][]byte{
		[]byte(""),
		[]byte("hello"),
		[]byte("LEETCODE_SESSION=abc123; csrftoken=xyz789"),
		bytes.Repeat([]byte("a"), 4096),
	}
	for i, in := range cases {
		blob, err := v.Seal(in)
		if err != nil {
			t.Fatalf("seal %d: %v", i, err)
		}
		out, err := v.Open(blob)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if !bytes.Equal(in, out) {
			t.Fatalf("case %d: got %q want %q", i, out, in)
		}
	}
}

func TestSealUsesFreshNonce(t *testing.T) {
	v := newTestVault(t)
	a, _ := v.Seal([]byte("same"))
	b, _ := v.Seal([]byte("same"))
	if bytes.Equal(a, b) {
		t.Fatal("two seals of same plaintext produced identical blobs (nonce reuse)")
	}
}

func TestOpenRejectsTampered(t *testing.T) {
	v := newTestVault(t)
	blob, _ := v.Seal([]byte("secret"))
	blob[len(blob)-1] ^= 0x01 // flip a tag bit
	if _, err := v.Open(blob); err == nil {
		t.Fatal("Open accepted tampered ciphertext")
	}
}

func TestOpenRejectsWrongVersion(t *testing.T) {
	v := newTestVault(t)
	blob, _ := v.Seal([]byte("x"))
	blob[0] = 0xFE
	if _, err := v.Open(blob); err == nil {
		t.Fatal("Open accepted unknown version byte")
	}
}

func TestNewRejectsShortKey(t *testing.T) {
	if _, err := New(make([]byte, 8)); err == nil {
		t.Fatal("New accepted short key")
	}
}
