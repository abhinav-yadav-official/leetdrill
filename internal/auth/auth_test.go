package auth

import (
	"errors"
	"testing"
)

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("hunter2")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash == "hunter2" {
		t.Fatal("hash is plaintext")
	}
	if err := VerifyPassword(hash, "hunter2"); err != nil {
		t.Fatalf("verify right pw: %v", err)
	}
	if err := VerifyPassword(hash, "nope"); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("wrong-pw error = %v, want ErrBadPassword", err)
	}
}

func TestHashPasswordRejectsEmpty(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Fatal("HashPassword accepted empty")
	}
}

func TestTokenRoundTrip(t *testing.T) {
	tok, h, err := NewToken()
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if len(tok) == 0 || len(h) != 32 {
		t.Fatalf("bad lengths tok=%d hash=%d", len(tok), len(h))
	}
	h2, err := HashToken(tok)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !EqualHashes(h, h2) {
		t.Fatal("hash mismatch: NewToken vs HashToken")
	}
}

func TestTokensAreUnique(t *testing.T) {
	a, _, _ := NewToken()
	b, _, _ := NewToken()
	if a == b {
		t.Fatal("two NewTokens collided")
	}
}

func TestHashTokenRejectsGarbage(t *testing.T) {
	if _, err := HashToken("not base64 !!!"); err == nil {
		t.Fatal("HashToken accepted garbage")
	}
}
