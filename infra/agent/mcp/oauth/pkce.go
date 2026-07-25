package oauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

type PKCE struct {
	Verifier  string
	Challenge string
}

func NewPKCE(randr io.Reader) (PKCE, error) {
	if randr == nil {
		randr = rand.Reader
	}
	raw := make([]byte, 32)
	if _, err := io.ReadFull(randr, raw); err != nil {
		return PKCE{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return PKCE{Verifier: verifier, Challenge: challenge}, nil
}

type State struct {
	Kind        string `json:"k"`
	WorkspaceID string `json:"ws"`
	BindingID   string `json:"b"`
	Verifier    string `json:"v"`
	IssuedAt    int64  `json:"t"`
	Nonce       string `json:"n"`
}

type Signer struct {
	key []byte
	now func() time.Time
	max time.Duration
}

func NewSigner(key []byte, max time.Duration) *Signer {
	if max <= 0 {
		max = 10 * time.Minute
	}
	return &Signer{key: key, now: time.Now, max: max}
}

func (s *Signer) Sign(st State) (string, error) {
	st.IssuedAt = s.now().Unix()
	if st.Nonce == "" {
		nb := make([]byte, 16)
		if _, err := io.ReadFull(rand.Reader, nb); err != nil {
			return "", err
		}
		st.Nonce = base64.RawURLEncoding.EncodeToString(nb)
	}
	payload, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (s *Signer) Verify(token string) (State, error) {
	var zero State
	sep := -1
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			sep = i
			break
		}
	}
	if sep < 1 || sep == len(token)-1 {
		return zero, errors.New("oauth: malformed state token")
	}
	payloadB, err := base64.RawURLEncoding.DecodeString(token[:sep])
	if err != nil {
		return zero, fmt.Errorf("oauth: bad payload: %w", err)
	}
	sigB, err := base64.RawURLEncoding.DecodeString(token[sep+1:])
	if err != nil {
		return zero, fmt.Errorf("oauth: bad signature: %w", err)
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write(payloadB)
	if !hmac.Equal(mac.Sum(nil), sigB) {
		return zero, errors.New("oauth: signature mismatch")
	}
	var st State
	if err := json.Unmarshal(payloadB, &st); err != nil {
		return zero, fmt.Errorf("oauth: bad json: %w", err)
	}
	if st.IssuedAt == 0 {
		return zero, errors.New("oauth: missing issuedAt")
	}
	age := s.now().Sub(time.Unix(st.IssuedAt, 0))
	if age < 0 || age > s.max {
		return zero, errors.New("oauth: state expired")
	}
	return st, nil
}
