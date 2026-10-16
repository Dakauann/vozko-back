package messagestatus

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

type Status string

const (
	StatusSent      Status = "sent"
	StatusDelivered Status = "delivered"
	StatusRead      Status = "read"
	StatusFailed    Status = "failed"
)

const (
	RawRetention    = 120 * 24 * time.Hour
	RecountHorizon  = RawRetention - 7*24*time.Hour
	AttributionWait = 2 * time.Hour
)

var ErrInvalidEvent = errors.New("message status event needs a message id, a phone number id, a known status and when it happened")

type Event struct {
	MessageID     string
	PhoneNumberID string
	Status        Status
	ErrorCode     int
	Pricing       string
	OccurredAt    time.Time
}

func ParseStatus(raw string) (Status, bool) {
	switch s := Status(strings.ToLower(strings.TrimSpace(raw))); s {
	case StatusSent, StatusDelivered, StatusRead, StatusFailed:
		return s, true
	default:
		return "", false
	}
}

func ParseOccurredAt(unixSeconds string) (time.Time, bool) {
	seconds, err := strconv.ParseInt(strings.TrimSpace(unixSeconds), 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(seconds, 0).UTC(), true
}

func (e Event) Validate() error {
	if strings.TrimSpace(e.MessageID) == "" || strings.TrimSpace(e.PhoneNumberID) == "" || e.OccurredAt.IsZero() {
		return ErrInvalidEvent
	}
	if _, ok := ParseStatus(string(e.Status)); !ok {
		return ErrInvalidEvent
	}
	return nil
}

type Recorder interface {
	Record(events ...Event) error
}

type SendingPhones interface {
	PhonesSendingSince(ctx context.Context, since time.Time) ([]string, error)
}

type Ledger interface {
	AttributePending(ctx context.Context, giveUpBefore time.Time) (int64, error)
	RollUpPending(ctx context.Context, recountAfter time.Time) (int64, error)
	PurgeRolledUpBefore(ctx context.Context, cutoff time.Time) (int64, error)
}
