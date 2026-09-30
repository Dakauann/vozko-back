package callrouting

import (
	"strings"
	"time"
	"unicode/utf8"
)

const MaxTransferNotes = 500

type TargetKind string

const (
	TargetQueue  TargetKind = "queue"
	TargetMember TargetKind = "member"
)

type TransferTarget struct {
	Kind    TargetKind
	QueueID string
	UserID  string
}

func (t TransferTarget) Validate() error {
	switch {
	case t.Kind == TargetQueue && strings.TrimSpace(t.QueueID) != "" && t.UserID == "":
		return nil
	case t.Kind == TargetMember && strings.TrimSpace(t.UserID) != "" && t.QueueID == "":
		return nil
	}
	return ErrInvalidTransferTarget
}

type TransferOutcome string

const (
	OutcomePending   TransferOutcome = "pending"
	OutcomeConnected TransferOutcome = "connected"
	OutcomeReturned  TransferOutcome = "returned"
	OutcomeTimedOut  TransferOutcome = "timed_out"
	OutcomeAbandoned TransferOutcome = "abandoned"
	OutcomeCancelled TransferOutcome = "cancelled"
)

type TransferRecord struct {
	ID          string
	WorkspaceID string
	CallID      string
	FromUserID  string
	Target      TransferTarget
	Notes       string
	Outcome     TransferOutcome
	AnsweredBy  string
	CreatedAt   time.Time
	FinishedAt  time.Time
}

func NormalizeTransferNotes(notes string) (string, error) {
	notes = strings.TrimSpace(notes)
	if utf8.RuneCountInString(notes) > MaxTransferNotes {
		return "", ErrTransferNotesTooLong
	}
	return notes, nil
}
