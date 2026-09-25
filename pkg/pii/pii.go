// Package pii encrypts personal data at rest with AES-256-GCM (LLD v2.0 §1).
//
// The associated data binds each ciphertext to its record and field, so a
// ciphertext copied into another row or column fails to decrypt.
package pii

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// ErrDecrypt is returned for tampered, truncated or mis-bound ciphertexts.
var ErrDecrypt = errors.New("pii: decryption failed")

// Seal encrypts plaintext with a 32-byte key; the output is nonce || ciphertext.
func Seal(key, plaintext, associatedData []byte) ([]byte, error) {
	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("pii: nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, plaintext, associatedData), nil
}

// Open decrypts the output of Seal.
func Open(key, sealed, associatedData []byte) ([]byte, error) {
	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrDecrypt
	}
	nonce, ct := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	out, err := aead.Open(nil, nonce, ct, associatedData)
	if err != nil {
		return nil, ErrDecrypt
	}
	return out, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("pii: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
