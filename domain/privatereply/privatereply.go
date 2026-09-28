package privatereply

import (
	"context"
	"errors"
	"time"

	"vozko/domain/shared"
)

const Window = 7 * 24 * time.Hour

var (
	ErrUsed            = errors.New("a private reply was already sent for this comment")
	ErrExpired         = errors.New("private replies must be sent within 7 days of the comment")
	ErrDeadlineUnknown = errors.New("the comment time is unknown, so the private reply deadline cannot be checked")
)

type Status string

const (
	StatusAttempted Status = "ATTEMPTED"
	StatusSent      Status = "SENT"
	StatusFailed    Status = "FAILED"
)

type Record struct {
	Source       shared.EntryType
	CommentID    string
	AccountID    string
	Status       Status
	RecipientRef *string
	MessageID    *string
	ErrorCode    int
	ErrorMessage string
	AttemptedAt  time.Time
	UpdatedAt    time.Time
}

func (r *Record) Consumed() bool {
	return r.Status == StatusSent || r.Status == StatusAttempted
}

func Deadline(commentedAt *time.Time) *time.Time {
	if commentedAt == nil || commentedAt.IsZero() {
		return nil
	}
	deadline := commentedAt.Add(Window)
	return &deadline
}

func CheckDeadline(commentedAt *time.Time, now time.Time) error {
	deadline := Deadline(commentedAt)
	if deadline == nil {
		return ErrDeadlineUnknown
	}
	if now.After(*deadline) {
		return ErrExpired
	}
	return nil
}

type Repository interface {
	Claim(ctx context.Context, source shared.EntryType, commentID, accountID string) (bool, error)
	MarkSent(ctx context.Context, source shared.EntryType, commentID, recipientRef, messageID string) error
	MarkFailed(ctx context.Context, source shared.EntryType, commentID string, code int, message string) error
	Find(ctx context.Context, source shared.EntryType, commentID string) (*Record, error)
	FindMany(ctx context.Context, source shared.EntryType, commentIDs []string) (map[string]*Record, error)
}
