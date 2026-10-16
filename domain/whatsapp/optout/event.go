package optout

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const (
	WebhookField       = "user_preferences"
	SubscriptionObject = "whatsapp_business_account"
)

type Preference string

const (
	PreferenceStop   Preference = "stop"
	PreferenceResume Preference = "resume"
)

var ErrInvalidEvent = errors.New("opt-out event needs the receiving phone, the recipient, a known preference and when it happened")

type Event struct {
	PhoneNumberID string
	RecipientHash string
	Category      string
	Preference    Preference
	OccurredAt    time.Time
}

func ParsePreference(raw string) (Preference, bool) {
	switch p := Preference(strings.ToLower(strings.TrimSpace(raw))); p {
	case PreferenceStop, PreferenceResume:
		return p, true
	default:
		return "", false
	}
}

func HashRecipient(waID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(waID)))
	return hex.EncodeToString(sum[:])
}

func (e Event) Validate() error {
	if strings.TrimSpace(e.PhoneNumberID) == "" || strings.TrimSpace(e.RecipientHash) == "" || e.OccurredAt.IsZero() {
		return ErrInvalidEvent
	}
	if _, ok := ParsePreference(string(e.Preference)); !ok {
		return ErrInvalidEvent
	}
	return nil
}

type Recorder interface {
	Record(events ...Event) error
}
