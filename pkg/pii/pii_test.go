package pii

import (
	"bytes"
	"errors"
	"testing"
)

var key = bytes.Repeat([]byte{7}, 32)

func TestSealOpenRoundTrip(t *testing.T) {
	aad := []byte("users.msisdn:018f")
	sealed, err := Seal(key, []byte("+254712345678"), aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("712345678")) {
		t.Fatal("plaintext visible in ciphertext")
	}
	got, err := Open(key, sealed, aad)
	if err != nil || string(got) != "+254712345678" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	again, _ := Seal(key, []byte("+254712345678"), aad)
	if bytes.Equal(sealed, again) {
		t.Error("two seals of the same value must differ (random nonce)")
	}
}

func TestOpenRejectsTamperingAndRebinding(t *testing.T) {
	aad := []byte("users.msisdn:a")
	sealed, _ := Seal(key, []byte("+254712345678"), aad)

	flipped := append([]byte(nil), sealed...)
	flipped[len(flipped)-1] ^= 1
	otherKey := bytes.Repeat([]byte{8}, 32)
	for name, try := range map[string]func() ([]byte, error){
		"tampered":  func() ([]byte, error) { return Open(key, flipped, aad) },
		"other row": func() ([]byte, error) { return Open(key, sealed, []byte("users.msisdn:b")) },
		"wrong key": func() ([]byte, error) { return Open(otherKey, sealed, aad) },
		"truncated": func() ([]byte, error) { return Open(key, sealed[:10], aad) },
	} {
		if _, err := try(); !errors.Is(err, ErrDecrypt) {
			t.Errorf("%s: err = %v, want ErrDecrypt", name, err)
		}
	}
}

func TestKeyLength(t *testing.T) {
	if _, err := Seal([]byte("short"), []byte("x"), nil); err == nil {
		t.Error("expected error for a short key")
	}
}
