package googleaccount

import (
	"fmt"
	"strings"
	"time"
)

const TokenRefreshLead = 10 * time.Minute

type Status string

const (
	StatusConnected      Status = "connected"
	StatusNeedsReconnect Status = "needs_reconnect"
	StatusDisconnected   Status = "disconnected"
)

func (s Status) Valid() bool {
	switch s {
	case StatusConnected, StatusNeedsReconnect, StatusDisconnected:
		return true
	}
	return false
}

func (s Status) CanTransitionTo(next Status) bool {
	if !s.Valid() || !next.Valid() {
		return false
	}
	if s == StatusDisconnected {
		return next != StatusNeedsReconnect
	}
	return true
}

type Account struct {
	ID              string
	WorkspaceID     string
	Email           string
	AccessToken     string
	RefreshToken    string
	AccessExpiresAt time.Time
	Scopes          []string
	Status          Status
	StatusReason    string
	ConnectedBy     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (a Account) NeedsRefresh(now time.Time) bool {
	return a.AccessExpiresAt.IsZero() || !now.Add(TokenRefreshLead).Before(a.AccessExpiresAt)
}

func (a Account) Usable() error {
	switch a.Status {
	case StatusConnected:
		if strings.TrimSpace(a.RefreshToken) == "" {
			return ErrNeedsReconnect
		}
		return nil
	case StatusNeedsReconnect:
		return ErrNeedsReconnect
	}
	return ErrNotConnected
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

type Binding struct {
	WorkspaceID string
	Product     Product
	AccountID   string
}

type BoundAccount struct {
	Product Product
	Account *Account
}

type Access struct {
	AccountID string
	Email     string
	Token     string
}

type Grant struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
	Scopes       []string
}

func (g Grant) Validate() error {
	if strings.TrimSpace(g.AccessToken) == "" {
		return fmt.Errorf("%w: access token", ErrIncompleteGrant)
	}
	if g.ExpiresIn <= 0 {
		return fmt.Errorf("%w: lifetime", ErrIncompleteGrant)
	}
	return nil
}

type Profile struct {
	Email string
	Name  string
}

type TokenUpdate struct {
	AccessToken     string
	RefreshToken    string
	AccessExpiresAt time.Time
	Scopes          []string
}

type RefreshOutcome struct {
	Tokens       *TokenUpdate
	Status       Status
	StatusReason string
}
