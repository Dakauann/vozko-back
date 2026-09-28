package oauthstate

import (
	"fmt"
	"strings"
	"time"
)

type KeyValueStore interface {
	SetNX(key, value string, ttl time.Duration) (bool, error)
	Exists(key string) (bool, error)
	Del(keys ...string) error
}

type NonceStore interface {
	Issue(nonce, workspaceID string) error
	Consume(nonce string) error
}

type sharedStateNonceStore struct {
	state  KeyValueStore
	prefix string
}

func NewNonceStore(state KeyValueStore, prefix string) (NonceStore, error) {
	if state == nil {
		return nil, fmt.Errorf("oauth: nonce store requires shared state")
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, fmt.Errorf("oauth: nonce store requires a key prefix")
	}
	return &sharedStateNonceStore{state: state, prefix: prefix}, nil
}

func (s *sharedStateNonceStore) issuedKey(nonce string) string { return s.prefix + ":nonce:" + nonce }
func (s *sharedStateNonceStore) usedKey(nonce string) string   { return s.prefix + ":used:" + nonce }

func (s *sharedStateNonceStore) Issue(nonce, workspaceID string) error {
	ok, err := s.state.SetNX(s.issuedKey(nonce), workspaceID, TTL)
	if err != nil {
		return fmt.Errorf("oauth: persist nonce: %w", err)
	}
	if !ok {
		return fmt.Errorf("oauth: nonce collision")
	}
	return nil
}

func (s *sharedStateNonceStore) Consume(nonce string) error {
	issued, err := s.state.Exists(s.issuedKey(nonce))
	if err != nil {
		return fmt.Errorf("oauth: verify nonce: %w", err)
	}
	if !issued {
		return ErrReplayedState
	}
	claimed, err := s.state.SetNX(s.usedKey(nonce), "1", TTL)
	if err != nil {
		return fmt.Errorf("oauth: consume nonce: %w", err)
	}
	if !claimed {
		return ErrReplayedState
	}
	_ = s.state.Del(s.issuedKey(nonce))
	return nil
}
