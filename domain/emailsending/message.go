package emailsending

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/emailaddress"
)

type MessageKind string

const (
	KindMarketing     MessageKind = "marketing"
	KindTransactional MessageKind = "transactional"
)

type RunKind string

const (
	RunCampaign  RunKind = "campaign"
	RunTriggered RunKind = "triggered"
	RunReply     RunKind = "reply"
	RunTest      RunKind = "test"
	RunAPI       RunKind = "api"
)

func (k RunKind) RoutesRepliesToInbox() bool { return k != RunAPI }

type Header struct {
	Name  string
	Value string
}

type OutboundMessage struct {
	TenantName  string
	From        Mailbox
	ReplyTo     []emailaddress.Address
	To          emailaddress.Address
	Subject     string
	HTML        string
	Text        string
	Headers     []Header
	Tags        map[string]string
	Kind        MessageKind
	Attachments []Attachment
}

type SendReceipt struct {
	ProviderMessageID string
}

func ListUnsubscribeHeaders(oneClickURL, mailto string) ([]Header, error) {
	if !strings.HasPrefix(oneClickURL, "https://") {
		return nil, ErrInsecureUnsubscribe
	}
	value := "<" + oneClickURL + ">"
	if strings.TrimSpace(mailto) != "" {
		value += ", <mailto:" + mailto + ">"
	}
	return []Header{
		{Name: "List-Unsubscribe", Value: value},
		{Name: "List-Unsubscribe-Post", Value: "List-Unsubscribe=One-Click"},
	}, nil
}

func (m OutboundMessage) Validate() error {
	switch {
	case strings.TrimSpace(m.TenantName) == "":
		return fmt.Errorf("%w: tenant", ErrInvalidMessage)
	case m.From.Address == "":
		return fmt.Errorf("%w: from", ErrInvalidMessage)
	case emailaddress.IsFreeMailbox(m.From.Address.Domain()):
		return ErrFreeMailboxFrom
	case m.To == "":
		return fmt.Errorf("%w: to", ErrInvalidMessage)
	case strings.TrimSpace(m.Subject) == "" || strings.ContainsAny(m.Subject, "\r\n"):
		return fmt.Errorf("%w: subject", ErrInvalidMessage)
	case strings.ContainsAny(m.From.Name, "\r\n"):
		return fmt.Errorf("%w: from name", ErrInvalidMessage)
	case m.HTML == "" && m.Text == "":
		return fmt.Errorf("%w: body", ErrInvalidMessage)
	case m.Kind != KindMarketing && m.Kind != KindTransactional:
		return fmt.Errorf("%w: kind", ErrInvalidMessage)
	}
	for _, h := range m.Headers {
		if strings.TrimSpace(h.Name) == "" || strings.ContainsAny(h.Name+h.Value, "\r\n") {
			return fmt.Errorf("%w: header %q", ErrInvalidMessage, h.Name)
		}
	}
	if m.Kind == KindMarketing && !hasHeader(m.Headers, "List-Unsubscribe") {
		return fmt.Errorf("%w: marketing needs List-Unsubscribe", ErrInvalidMessage)
	}
	return CheckAttachments(m.Attachments)
}

func hasHeader(headers []Header, name string) bool {
	for _, h := range headers {
		if strings.EqualFold(h.Name, name) {
			return true
		}
	}
	return false
}

type MessageStatus string

const (
	MessageQueued     MessageStatus = "queued"
	MessageSent       MessageStatus = "sent"
	MessageDelivered  MessageStatus = "delivered"
	MessageBounced    MessageStatus = "bounced"
	MessageSuppressed MessageStatus = "suppressed"
	MessageFailed     MessageStatus = "failed"
)

type Message struct {
	ID                string
	WorkspaceID       string
	ProviderMessageID string
	RunKind           RunKind
	RunID             string
	RecipientID       string
	DomainID          string
	Address           emailaddress.Address
	Status            MessageStatus
	SentAt            time.Time
	DeliveredAt       *time.Time
	FirstOpenAt       *time.Time
	FirstClickAt      *time.Time
	BouncedAt         *time.Time
	BounceKind        BounceKind
	ComplainedAt      *time.Time
	UnsubscribedAt    *time.Time
	Opens             int
	BotOpens          int
	Clicks            int
}

type SuppressReason string

const (
	SuppressHardBounce   SuppressReason = "hard_bounce"
	SuppressComplaint    SuppressReason = "complaint"
	SuppressManual       SuppressReason = "manual"
	SuppressSoftBounce   SuppressReason = "soft_bounce"
	SuppressBadAddress   SuppressReason = "bad_address"
	SuppressNoMailDomain SuppressReason = "no_mail_domain"
)

func StoredSuppressReasons() []SuppressReason {
	return []SuppressReason{SuppressHardBounce, SuppressComplaint, SuppressManual, SuppressSoftBounce}
}

func (r SuppressReason) Stored() bool {
	for _, known := range StoredSuppressReasons() {
		if r == known {
			return true
		}
	}
	return false
}

func (r SuppressReason) Removable() bool {
	return r == SuppressManual || r == SuppressSoftBounce
}

type EventEffect struct {
	Changed        bool
	Delivered      bool
	HardBounced    bool
	Complained     bool
	FirstOpen      bool
	FirstClick     bool
	Suppress       bool
	SuppressReason SuppressReason
	Unsubscribe    bool
}

func ApplyEvent(m *Message, ev DeliveryEvent) EventEffect {
	switch ev.Kind {
	case EventDelivery:
		return applyDelivery(m, ev)
	case EventOpen:
		return applyOpen(m, ev)
	case EventClick:
		return applyClick(m, ev)
	case EventBounce:
		return applyBounce(m, ev)
	case EventComplaint:
		return applyComplaint(m, ev)
	case EventReject, EventRenderingFailure:
		if m.Status == MessageSent {
			m.Status = MessageFailed
			return EventEffect{Changed: true}
		}
	}
	return EventEffect{}
}

func applyDelivery(m *Message, ev DeliveryEvent) EventEffect {
	if m.Status != MessageSent {
		return EventEffect{}
	}
	m.Status = MessageDelivered
	m.DeliveredAt = stamp(ev.OccurredAt)
	return EventEffect{Changed: true, Delivered: true}
}

func applyOpen(m *Message, ev DeliveryEvent) EventEffect {
	if ev.IsBot {
		m.BotOpens++
		return EventEffect{Changed: true}
	}
	m.Opens++
	effect := EventEffect{Changed: true}
	if m.FirstOpenAt == nil {
		m.FirstOpenAt = stamp(ev.OccurredAt)
		effect.FirstOpen = true
	}
	return effect
}

func applyClick(m *Message, ev DeliveryEvent) EventEffect {
	if ev.IsBot {
		return EventEffect{}
	}
	m.Clicks++
	effect := EventEffect{Changed: true}
	if m.FirstClickAt == nil {
		m.FirstClickAt = stamp(ev.OccurredAt)
		effect.FirstClick = true
	}
	if m.FirstOpenAt == nil {
		m.FirstOpenAt = stamp(ev.OccurredAt)
		effect.FirstOpen = true
	}
	return effect
}

func applyBounce(m *Message, ev DeliveryEvent) EventEffect {
	kind := BounceSoft
	if ev.Bounce != nil {
		kind = ev.Bounce.Kind
	}
	firstHard := kind == BounceHard && m.BounceKind != BounceHard
	m.BouncedAt = stamp(ev.OccurredAt)
	m.BounceKind = kind
	if kind == BounceSuppressed {
		m.Status = MessageSuppressed
		return EventEffect{Changed: true}
	}
	m.Status = MessageBounced
	if kind == BounceHard {
		return EventEffect{Changed: true, Suppress: true, SuppressReason: SuppressHardBounce, HardBounced: firstHard}
	}
	return EventEffect{Changed: true}
}

func applyComplaint(m *Message, ev DeliveryEvent) EventEffect {
	if ev.Complaint != nil && strings.EqualFold(ev.Complaint.FeedbackType, "not-spam") {
		return EventEffect{}
	}
	first := m.ComplainedAt == nil
	if first {
		m.ComplainedAt = stamp(ev.OccurredAt)
	}
	return EventEffect{Changed: true, Suppress: true, SuppressReason: SuppressComplaint, Unsubscribe: true, Complained: first}
}

func stamp(t time.Time) *time.Time {
	v := t
	return &v
}
