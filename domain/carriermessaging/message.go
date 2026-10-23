package carriermessaging

import (
	"errors"
	"strings"
	"time"
	"vozko/domain/rcs"
)

const (
	DefaultValidity  = 24 * time.Hour
	FallbackValidity = 30 * time.Minute
	referenceLength  = 32
)

var (
	ErrReferenceInvalid = errors.New("rcs: a message reference is 32 letters and numbers")
	ErrMessageNotFound  = errors.New("rcs: message not found")
)

func ReferenceFor(messageID string) (string, error) {
	ref := strings.ReplaceAll(strings.TrimSpace(messageID), "-", "")
	if len(ref) != referenceLength {
		return "", ErrReferenceInvalid
	}
	for _, r := range ref {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return "", ErrReferenceInvalid
		}
	}
	return ref, nil
}

type Status string

const (
	StatusPending   Status = "pending"
	StatusAccepted  Status = "accepted"
	StatusDelivered Status = "delivered"
	StatusRead      Status = "read"
	StatusRejected  Status = "rejected"
	StatusFailed    Status = "failed"
	StatusExpired   Status = "expired"
)

var progressRank = map[Status]int{
	StatusPending:   0,
	StatusAccepted:  1,
	StatusDelivered: 2,
	StatusRead:      3,
}

func (s Status) Valid() bool {
	_, progress := progressRank[s]
	return progress || s.Terminal()
}

func (s Status) Terminal() bool {
	return s == StatusRejected || s == StatusFailed || s == StatusExpired
}

func (s Status) Landed() bool {
	return s == StatusDelivered || s == StatusRead
}

func (s Status) Supersedes(next Status) bool {
	if s == next {
		return false
	}
	if next.Terminal() {
		return !s.Terminal() && !s.Landed()
	}
	if s.Terminal() {
		return next.Landed()
	}
	return progressRank[next] > progressRank[s]
}

type Step string

const (
	StepRCS      Step = "rcs"
	StepWhatsApp Step = "whatsapp"
	StepSMS      Step = "sms"
)

type FallbackPlan struct {
	WhatsApp bool
	SMS      bool
}

type WhatsAppFallback struct {
	ActorID         string   `json:"actor_id"`
	BusinessPhoneID string   `json:"business_phone_id"`
	TemplateID      string   `json:"template_id"`
	BodyParams      []string `json:"body_params,omitempty"`
	HeaderParams    []string `json:"header_params,omitempty"`
}

type FallbackSpec struct {
	WhatsApp   *WhatsAppFallback `json:"whatsapp,omitempty"`
	SMSRouteID string            `json:"sms_route_id,omitempty"`
	SMSText    string            `json:"sms_text,omitempty"`
}

func (s FallbackSpec) Plan() FallbackPlan {
	return FallbackPlan{WhatsApp: s.WhatsApp != nil, SMS: strings.TrimSpace(s.SMSText) != "" && strings.TrimSpace(s.SMSRouteID) != ""}
}

func (p FallbackPlan) Enabled() bool {
	return p.WhatsApp || p.SMS
}

func (p FallbackPlan) Validity() time.Duration {
	if p.Enabled() {
		return FallbackValidity
	}
	return DefaultValidity
}

func (p FallbackPlan) Next(step Step, status Status) (Step, bool) {
	if !status.Terminal() {
		return "", false
	}
	switch step {
	case StepRCS:
		if p.WhatsApp {
			return StepWhatsApp, true
		}
		if p.SMS {
			return StepSMS, true
		}
	case StepWhatsApp:
		if p.SMS {
			return StepSMS, true
		}
	}
	return "", false
}

type RunKind string

const (
	RunCampaign   RunKind = "campaign"
	RunAutomation RunKind = "automation"
	RunOperator   RunKind = "operator"
	RunWorkflow   RunKind = "workflow"
	RunTest       RunKind = "test"
)

type Message struct {
	ID                string
	WorkspaceID       string
	Channel           Channel
	Reference         string
	SenderID          string
	Phone             string
	LeadID            string
	ConversationID    string
	RunKind           RunKind
	Marketing         bool
	RunID             string
	Variant           int
	Content           rcs.Content
	Text              string
	Segments          int
	Class             rcs.BillingClass
	Step              Step
	Status            Status
	ErrorCode         string
	ParentMessageID   string
	FallbackMessageID string
	Fallback          FallbackSpec
	ChargeReference   string
	AcceptedAt        *time.Time
	DeliveredAt       *time.Time
	ReadAt            *time.Time
	FailedAt          *time.Time
	ExpiresAt         *time.Time
	CreatedAt         time.Time
}

func (m Message) Advance(next Status, errorCode string, at time.Time) (Message, bool) {
	if !next.Valid() || !m.Status.Supersedes(next) {
		return m, false
	}
	stamp := func(field **time.Time) {
		if *field == nil {
			t := at
			*field = &t
		}
	}
	switch next {
	case StatusAccepted:
		stamp(&m.AcceptedAt)
	case StatusDelivered:
		stamp(&m.AcceptedAt)
		stamp(&m.DeliveredAt)
	case StatusRead:
		stamp(&m.AcceptedAt)
		stamp(&m.DeliveredAt)
		stamp(&m.ReadAt)
	case StatusRejected, StatusFailed, StatusExpired:
		stamp(&m.FailedAt)
		m.ErrorCode = strings.TrimSpace(errorCode)
	}
	m.Status = next
	return m, true
}

func (m Message) Overdue(now time.Time) bool {
	return m.ExpiresAt != nil && !now.Before(*m.ExpiresAt) && !m.Status.Landed() && !m.Status.Terminal()
}
