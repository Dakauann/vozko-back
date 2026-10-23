package carriercampaign

import (
	"errors"
	"strings"
	"time"

	"vozko/domain/campaign"
)

var (
	ErrRunNotFound       = errors.New("carriercampaign: run not found")
	ErrRunFinished       = errors.New("carriercampaign: the run already finished")
	ErrRecipientNotFound = errors.New("carriercampaign: recipient not found")
	ErrRecipientSettled  = errors.New("carriercampaign: the recipient was already handled")
	ErrSkipReasonInvalid = errors.New("carriercampaign: unknown skip reason")
	ErrMessageIDRequired = errors.New("carriercampaign: a sent recipient needs its message")
	ErrErrorCodeRequired = errors.New("carriercampaign: a failed recipient needs an error code")
)

type Run struct {
	ID          string
	WorkspaceID string
	CampaignID  string
	Occurrence  time.Time
	Status      campaign.Status
	StartedAt   time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func runFinished(s campaign.Status) bool {
	s = campaign.NormalizeStatus(s)
	return s == campaign.StatusCompleted || s == campaign.StatusStopped
}

func (r Run) Apply(action campaign.Action) (Run, error) {
	if runFinished(r.Status) {
		return r, ErrRunFinished
	}
	transition, err := campaign.ResolveTransition(r.Status, action)
	if err != nil {
		return r, err
	}
	r.Status = transition.Target
	return r, nil
}

func (r Run) Complete(at time.Time) (Run, error) {
	if runFinished(r.Status) {
		return r, ErrRunFinished
	}
	if campaign.NormalizeStatus(r.Status) != campaign.StatusRunning {
		return r, campaign.ErrNotRunning
	}
	r.Status = campaign.StatusCompleted
	r.CompletedAt = &at
	return r, nil
}

type RecipientStatus string

const (
	RecipientPending RecipientStatus = "pending"
	RecipientSent    RecipientStatus = "sent"
	RecipientSkipped RecipientStatus = "skipped"
	RecipientFailed  RecipientStatus = "failed"
)

func AllRecipientStatuses() []RecipientStatus {
	return []RecipientStatus{RecipientPending, RecipientSent, RecipientSkipped, RecipientFailed}
}

func (s RecipientStatus) Valid() bool {
	for _, known := range AllRecipientStatuses() {
		if s == known {
			return true
		}
	}
	return false
}

type Recipient struct {
	ID          string
	WorkspaceID string
	CampaignID  string
	RunID       string
	LeadID      string
	Phone       string
	Variant     int
	Status      RecipientStatus
	SkipReason  SkipReason
	MessageID   string
	ErrorCode   string
	SentAt      *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (r Recipient) MarkSent(messageID string, at time.Time) (Recipient, error) {
	if r.Status != RecipientPending {
		return r, ErrRecipientSettled
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return r, ErrMessageIDRequired
	}
	r.Status = RecipientSent
	r.MessageID = messageID
	r.SentAt = &at
	return r, nil
}

func (r Recipient) Skip(reason SkipReason) (Recipient, error) {
	if r.Status != RecipientPending {
		return r, ErrRecipientSettled
	}
	if !reason.Valid() {
		return r, ErrSkipReasonInvalid
	}
	r.Status = RecipientSkipped
	r.SkipReason = reason
	return r, nil
}

func (r Recipient) Fail(errorCode string) (Recipient, error) {
	if r.Status != RecipientPending {
		return r, ErrRecipientSettled
	}
	errorCode = strings.TrimSpace(errorCode)
	if errorCode == "" {
		return r, ErrErrorCodeRequired
	}
	r.Status = RecipientFailed
	r.ErrorCode = errorCode
	return r, nil
}

type RecipientCounts struct {
	Total   int64                `json:"total"`
	Pending int64                `json:"pending"`
	Sent    int64                `json:"sent"`
	Failed  int64                `json:"failed"`
	Skipped map[SkipReason]int64 `json:"skipped"`
}

func (c RecipientCounts) SkippedTotal() int64 {
	var total int64
	for _, n := range c.Skipped {
		total += n
	}
	return total
}
