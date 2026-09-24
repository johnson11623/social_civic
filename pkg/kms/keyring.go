// Package kms abstracts access to versioned secret keys (peppers, signing keys).
//
// Production uses Vault/AWS KMS (T-X.4); Static is for local development and
// tests only and must not be used in production (security finding F-01).
package kms

import (
	"context"
	"errors"
	"sync"
)

// ErrKeyNotFound is returned when a named key is not in the keyring.
var ErrKeyNotFound = errors.New("kms: key not found")

// Key is secret key material with the version that produced it.
type Key struct {
	Material []byte
	Version  string
}

// Keyring returns the current version of a named key.
type Keyring interface {
	Current(ctx context.Context, name string) (Key, error)
}

// Static is an in-memory Keyring for development and tests.
type Static struct {
	mu   sync.RWMutex
	keys map[string]Key
}

// NewStatic returns an empty in-memory keyring.
func NewStatic() *Static {
	return &Static{keys: make(map[string]Key)}
}

// Set stores key material under name as the current version.
func (s *Static) Set(name string, material []byte, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[name] = Key{Material: append([]byte(nil), material...), Version: version}
}

// Current implements Keyring.
func (s *Static) Current(_ context.Context, name string) (Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.keys[name]
	if !ok {
		return Key{}, ErrKeyNotFound
	}
	return k, nil
}
