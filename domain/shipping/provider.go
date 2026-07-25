package shipping

import (
	"encoding/json"
	"time"
)

type Provider string

const ProviderMelhorEnvio Provider = "melhorenvio"

type ProviderToken struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scopes       []string
	ExpiresAt    time.Time
}

type ProviderAccount struct {
	ID          string
	UserID      string
	Provider    Provider
	ExternalID  string
	Label       string
	Token       ProviderToken
	AppSettings json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (a *ProviderAccount) BelongsTo(userID string) bool {
	if a == nil {
		return false
	}
	return a.UserID == userID
}

func (a *ProviderAccount) NeedsRefresh(now time.Time, tolerance time.Duration) bool {
	if a == nil {
		return true
	}
	if a.Token.ExpiresAt.IsZero() {
		return false
	}
	return !a.Token.ExpiresAt.Add(-tolerance).After(now)
}
