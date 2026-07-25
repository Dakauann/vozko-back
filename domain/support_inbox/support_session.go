package support_inbox

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrSessionNotFound = errors.New("support session not found")
	ErrSessionExpired  = errors.New("support session has expired")
	ErrSessionInvalid  = errors.New("invalid session token")
)

const (
	SessionTTL = 24 * time.Hour
)

type SupportSession struct {
	ID        string    `json:"id"`
	InboxID   string    `json:"inboxId"`
	EntryID   string    `json:"entryId"`
	Token     string    `json:"-"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *SupportSession) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

type SessionRepository interface {
	Create(session *SupportSession) error
	FindByToken(token string) (*SupportSession, error)
	DeleteExpired() (int64, error)
}

func GenerateSessionToken(secret string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	payload := hex.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

func ValidateSessionToken(token, secret string) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(parts[1]), []byte(expected))
}

type CreateSessionUseCase interface {
	Execute(input CreateSessionInput) (*CreateSessionOutput, error)
}

type CreateSessionInput struct {
	InboxID      string `json:"inboxId"`
	ContactName  string `json:"contactName,omitempty"`
	ContactEmail string `json:"contactEmail,omitempty"`
	SourceURL    string `json:"sourceUrl,omitempty"`
	Origin       string `json:"-"`
}

type CreateSessionOutput struct {
	SessionToken    string         `json:"sessionToken"`
	EntryID         string         `json:"entryId"`
	GreetingMessage string         `json:"greetingMessage,omitempty"`
	PreChatFields   []PreChatField `json:"preChatFields,omitempty"`
	ExpiresAt       time.Time      `json:"expiresAt"`
}

type ReconnectSessionUseCase interface {
	Execute(input ReconnectSessionInput) (*ReconnectSessionOutput, error)
}

type ReconnectSessionInput struct {
	SessionToken string `json:"sessionToken"`
	Origin       string `json:"-"`
}

type ReconnectSessionOutput struct {
	EntryID         string    `json:"entryId"`
	InboxID         string    `json:"inboxId"`
	GreetingMessage string    `json:"greetingMessage,omitempty"`
	ExpiresAt       time.Time `json:"expiresAt"`
}
