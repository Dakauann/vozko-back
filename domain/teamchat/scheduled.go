package teamchat

import (
	"strings"
	"time"

	"vozko/domain/shared"
)

type ScheduledStatus string

const (
	ScheduledPending  ScheduledStatus = "pending"
	ScheduledSent     ScheduledStatus = "sent"
	ScheduledCanceled ScheduledStatus = "canceled"
	ScheduledFailed   ScheduledStatus = "failed"
)

func (s ScheduledStatus) Final() bool {
	return s != ScheduledPending
}

type FailureReason string

const (
	ReasonNotMember            FailureReason = "not_member"
	ReasonRecipientUnavailable FailureReason = "recipient_unavailable"
	ReasonLeftWorkspace        FailureReason = "left_workspace"
	ReasonThreadUnavailable    FailureReason = "thread_unavailable"
)

const scheduledClientIDPrefix = "scheduled:"

type Scheduled struct {
	ID            string
	WorkspaceID   string
	ThreadID      string
	AuthorID      string
	Body          string
	Mentions      []string
	ReplyToID     string
	SendAt        time.Time
	Status        ScheduledStatus
	FailureReason FailureReason
	SentMessageID string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func ValidateSendAt(at, now time.Time) error {
	if at.Before(now.Add(shared.MessageScheduleLead)) {
		return ErrSendAtTooSoon
	}
	if at.After(now.Add(shared.MessageScheduleHorizon)) {
		return ErrSendAtTooFar
	}
	return nil
}

func NewScheduled(id string, t Thread, authorID string, d Draft, sendAt, now time.Time) (Scheduled, error) {
	d.ClientID = ""
	clean, err := d.normalized()
	if err != nil {
		return Scheduled{}, err
	}
	if err := ValidateSendAt(sendAt, now); err != nil {
		return Scheduled{}, err
	}
	at := now.UTC()
	return Scheduled{
		ID:          strings.TrimSpace(id),
		WorkspaceID: t.WorkspaceID,
		ThreadID:    t.ID,
		AuthorID:    strings.TrimSpace(authorID),
		Body:        clean.Body,
		Mentions:    clean.Mentions,
		ReplyToID:   clean.ReplyToID,
		SendAt:      sendAt.UTC(),
		Status:      ScheduledPending,
		CreatedAt:   at,
		UpdatedAt:   at,
	}, nil
}

func (s Scheduled) Draft() Draft {
	return Draft{Body: s.Body, Mentions: s.Mentions, ReplyToID: s.ReplyToID, ClientID: scheduledClientIDPrefix + s.ID}
}

func (s Scheduled) Due(now time.Time) bool {
	return s.Status == ScheduledPending && !s.SendAt.After(now)
}

func (s Scheduled) RescheduleBy(userID string, body *string, sendAt *time.Time, now time.Time) (Scheduled, error) {
	if err := s.changeableBy(userID); err != nil {
		return Scheduled{}, err
	}
	if body != nil {
		clean, err := NormalizeBody(*body)
		if err != nil {
			return Scheduled{}, err
		}
		s.Body = clean
	}
	if sendAt != nil {
		if err := ValidateSendAt(*sendAt, now); err != nil {
			return Scheduled{}, err
		}
		s.SendAt = sendAt.UTC()
	}
	s.UpdatedAt = now.UTC()
	return s, nil
}

func (s Scheduled) CancelBy(userID string, now time.Time) (Scheduled, error) {
	if err := s.changeableBy(userID); err != nil {
		return Scheduled{}, err
	}
	return s.moved(ScheduledCanceled, now), nil
}

func (s Scheduled) Sent(messageID string, now time.Time) (Scheduled, error) {
	if s.Status.Final() {
		return Scheduled{}, ErrScheduledFinal
	}
	s.SentMessageID = messageID
	return s.moved(ScheduledSent, now), nil
}

func (s Scheduled) Failed(reason FailureReason, now time.Time) (Scheduled, error) {
	if s.Status.Final() {
		return Scheduled{}, ErrScheduledFinal
	}
	s.FailureReason = reason
	return s.moved(ScheduledFailed, now), nil
}

func (s Scheduled) changeableBy(userID string) error {
	if strings.TrimSpace(userID) == "" || userID != s.AuthorID {
		return ErrNotAuthor
	}
	if s.Status.Final() {
		return ErrScheduledFinal
	}
	return nil
}

func (s Scheduled) moved(status ScheduledStatus, now time.Time) Scheduled {
	s.Status = status
	s.UpdatedAt = now.UTC()
	return s
}
