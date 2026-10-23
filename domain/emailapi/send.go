package emailapi

import (
	"fmt"
	"time"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
)

const (
	LeaseDuration   = 5 * time.Minute
	ProcessingLimit = 4 * time.Minute
)

type MessageStatus string

const (
	StatusPending    MessageStatus = "pending"
	StatusRefused    MessageStatus = "refused"
	StatusQueued     MessageStatus = MessageStatus(es.MessageQueued)
	StatusSent       MessageStatus = MessageStatus(es.MessageSent)
	StatusDelivered  MessageStatus = MessageStatus(es.MessageDelivered)
	StatusBounced    MessageStatus = MessageStatus(es.MessageBounced)
	StatusSuppressed MessageStatus = MessageStatus(es.MessageSuppressed)
	StatusFailed     MessageStatus = MessageStatus(es.MessageFailed)
)

var Statuses = []MessageStatus{StatusPending, StatusRefused, StatusQueued, StatusSent, StatusDelivered, StatusBounced, StatusSuppressed, StatusFailed}

func (s MessageStatus) Valid() bool {
	for _, known := range Statuses {
		if s == known {
			return true
		}
	}
	return false
}

func StatusOf(m es.MessageStatus) MessageStatus { return MessageStatus(m) }

type Send struct {
	ID                  string
	WorkspaceID         string
	IdempotencyKey      string
	Fingerprint         string
	Origin              Origin
	APIKeyID            string
	SenderID            string
	From                emailaddress.Address
	Subject             string
	Tags                map[string]string
	Recipients          int
	Round               int
	RoundUnits          int64
	RoundUnitMicros     int64
	RoundUnitCostMicros int64
	RoundSettled        bool
	LeaseUntil          *time.Time
	CreatedAt           time.Time
}

func (s Send) RoundOpen() bool { return s.Round > 0 && !s.RoundSettled }

func (s Send) LeaseHeld(now time.Time) bool { return s.LeaseUntil != nil && s.LeaseUntil.After(now) }

func (s Send) UnitsToRefund(messages []Message) int64 {
	if !s.RoundOpen() {
		return 0
	}
	var out int64
	for _, m := range messages {
		if m.Round == s.Round && m.WentOut() {
			out++
		}
	}
	return max(s.RoundUnits-out, 0)
}

func RoundReference(sendID string, round int) string {
	return fmt.Sprintf("email_api:%s#%d", sendID, round)
}

type Message struct {
	ID          string
	SendID      string
	WorkspaceID string
	Position    int
	Role        Role
	Address     emailaddress.Address
	Status      MessageStatus
	ErrorCode   string
	MessageID   string
	Round       int
	LastEvent   string
	LastEventAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (m Message) WentOut() bool {
	if m.MessageID == "" {
		return false
	}
	switch m.Status {
	case StatusQueued, StatusSent, StatusDelivered, StatusBounced, StatusSuppressed:
		return true
	}
	return false
}

func Outstanding(messages []Message) []Message {
	var out []Message
	for _, m := range messages {
		if !m.WentOut() {
			out = append(out, m)
		}
	}
	return out
}

func RecipientID(position int) string { return fmt.Sprintf("r%d", position) }
