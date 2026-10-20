package oauthstate

import (
	"fmt"
	"strings"
	"time"
)

type SealedValueStore interface {
	SetNX(key, value string, ttl time.Duration) (bool, error)
	GetString(key string) (string, error)
	Del(keys ...string) error
}

type SealedIssuer struct {
	issuer *Issuer
	store  SealedValueStore
	prefix string
}

func NewSealedIssuer(issuer *Issuer, store SealedValueStore, prefix string) (*SealedIssuer, error) {
	if issuer == nil {
		return nil, fmt.Errorf("oauth: sealed issuer requires a state issuer")
	}
	if store == nil {
		return nil, fmt.Errorf("oauth: sealed issuer requires a value store")
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, fmt.Errorf("oauth: sealed issuer requires a key prefix")
	}
	return &SealedIssuer{issuer: issuer, store: store, prefix: prefix}, nil
}

func (s *SealedIssuer) key(nonce string) string { return s.prefix + ":sealed:" + nonce }

func (s *SealedIssuer) Issue(in IssueInput, sealed string) (string, error) {
	if strings.TrimSpace(sealed) == "" {
		return "", fmt.Errorf("oauth: a sealed value is required")
	}
	raw, nonce, err := s.issuer.issue(in)
	if err != nil {
		return "", err
	}
	stored, err := s.store.SetNX(s.key(nonce), sealed, TTL)
	if err != nil {
		return "", fmt.Errorf("oauth: persist sealed value: %w", err)
	}
	if !stored {
		return "", fmt.Errorf("oauth: sealed value collision")
	}
	return raw, nil
}

func (s *SealedIssuer) Redeem(raw string) (*OAuthState, string, error) {
	state, err := s.issuer.Redeem(raw)
	if err != nil {
		return nil, "", err
	}
	key := s.key(state.Nonce)
	sealed, err := s.store.GetString(key)
	if err != nil {
		return nil, "", fmt.Errorf("oauth: read sealed value: %w", err)
	}
	_ = s.store.Del(key)
	if sealed == "" {
		return nil, "", ErrInvalidState
	}
	return state, sealed, nil
}

func (s *SealedIssuer) ReturnPath(state *OAuthState) string { return s.issuer.ReturnPath(state) }

func (s *SealedIssuer) DefaultReturnPath() string { return s.issuer.DefaultReturnPath() }
