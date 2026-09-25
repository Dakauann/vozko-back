package scheduled_message

import (
	"strings"
	"time"

	"vozko/domain/shared"
)

const (
	MinScheduleLead = time.Minute

	MaxScheduleHorizon = 30 * 24 * time.Hour
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusSending  Status = "sending"
	StatusSent     Status = "sent"
	StatusFailed   Status = "failed"
	StatusCanceled Status = "canceled"
)

func (s Status) IsTerminal() bool {
	switch s {
	case StatusSent, StatusFailed, StatusCanceled:
		return true
	default:
		return false
	}
}

func (s Status) CanTransitionTo(next Status) bool {
	switch s {
	case StatusPending:
		return next == StatusSending || next == StatusCanceled || next == StatusFailed
	case StatusSending:
		return next == StatusSent || next == StatusFailed
	default:
		return false
	}
}

type FailureReason string

const (
	ReasonWindowClosed        FailureReason = "window_closed"
	ReasonEntryUnavailable    FailureReason = "entry_unavailable"
	ReasonProviderError       FailureReason = "provider_error"
	ReasonDispatchInterrupted FailureReason = "dispatch_interrupted"

	ReasonPermissionRevoked   FailureReason = "permission_revoked"
	ReasonTemplateUnavailable FailureReason = "template_unavailable"
	ReasonContactIneligible   FailureReason = "contact_ineligible"
	ReasonInsufficientBalance FailureReason = "insufficient_balance"
	ReasonBillingUnavailable  FailureReason = "billing_unavailable"
	ReasonOutcomeUnknown      FailureReason = "outcome_unknown"
)

type Kind string

const (
	KindText     Kind = "text"
	KindTemplate Kind = "template"
)

func (k Kind) IsKnown() bool {
	return k == KindText || k == KindTemplate
}

func (k Kind) RequiresWindow() bool {
	return k != KindTemplate
}

func (k Kind) LatestAllowed(open bool, expiresAt *time.Time, now time.Time) (time.Time, error) {
	horizon := now.Add(MaxScheduleHorizon)
	if !k.RequiresWindow() {
		return horizon, nil
	}
	if !open {
		return time.Time{}, ErrWindowClosed
	}
	if expiresAt != nil && expiresAt.Before(horizon) {
		return *expiresAt, nil
	}
	return horizon, nil
}

func (k Kind) ValidateScheduledAt(at time.Time, open bool, expiresAt *time.Time, now time.Time) error {
	latest, err := k.LatestAllowed(open, expiresAt, now)
	if err != nil {
		return err
	}
	if at.Before(now.Add(MinScheduleLead)) {
		return ErrScheduledAtTooSoon
	}
	if at.After(latest) {
		if k.RequiresWindow() && expiresAt != nil && at.After(*expiresAt) {
			return ErrScheduledAtPastWindow
		}
		return ErrScheduledAtTooFar
	}
	return nil
}

type TemplateContent struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Preview      string   `json:"preview,omitempty"`
	BodyParams   []string `json:"bodyParams,omitempty"`
	HeaderParams []string `json:"headerParams,omitempty"`
}

type ScheduledMessage struct {
	ID          string           `json:"id"`
	WorkspaceID string           `json:"workspaceId"`
	EntryID     string           `json:"entryId"`
	EntryType   shared.EntryType `json:"entryType"`

	CreatedByUserID string `json:"createdByUserId"`

	Kind     Kind             `json:"kind"`
	Template *TemplateContent `json:"template,omitempty"`

	Text             string  `json:"text,omitempty"`
	MediaID          *string `json:"mediaId,omitempty"`
	MediaType        *string `json:"mediaType,omitempty"`
	ReplyToMessageID *string `json:"replyToMessageId,omitempty"`

	Signed bool `json:"signed"`

	ScheduledAt time.Time `json:"scheduledAt"`

	WindowExpiresAtAtCreation *time.Time `json:"windowExpiresAtAtCreation,omitempty"`

	Status        Status         `json:"status"`
	FailureReason *FailureReason `json:"failureReason,omitempty"`
	FailureDetail string         `json:"failureDetail,omitempty"`

	ClaimedAt     *time.Time `json:"claimedAt,omitempty"`
	SentAt        *time.Time `json:"sentAt,omitempty"`
	SentMessageID *string    `json:"sentMessageId,omitempty"`

	IdempotencyKey *string `json:"idempotencyKey,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (m *ScheduledMessage) Normalize() {
	m.Text = strings.TrimSpace(m.Text)
	m.MediaID = trimmedOrNil(m.MediaID)
	m.MediaType = trimmedOrNil(m.MediaType)
	m.ReplyToMessageID = trimmedOrNil(m.ReplyToMessageID)
	m.IdempotencyKey = trimmedOrNil(m.IdempotencyKey)
	if m.Kind == "" {
		m.Kind = KindText
	}
	if m.Template != nil {
		m.Template.ID = strings.TrimSpace(m.Template.ID)
	}
	if m.Status == "" {
		m.Status = StatusPending
	}
}

func (m *ScheduledMessage) Validate() error {
	if strings.TrimSpace(m.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	if strings.TrimSpace(m.EntryID) == "" {
		return ErrEntryIDRequired
	}
	if !m.EntryType.IsKnown() {
		return ErrEntryTypeInvalid
	}
	if strings.TrimSpace(m.CreatedByUserID) == "" {
		return ErrSenderRequired
	}
	switch m.Kind {
	case KindText:
		return m.validateText()
	case KindTemplate:
		return m.validateTemplate()
	default:
		return ErrKindInvalid
	}
}

func (m *ScheduledMessage) validateText() error {
	if m.Template != nil {
		return ErrKindMismatch
	}
	if m.Text == "" && !m.HasMedia() {
		return ErrContentRequired
	}
	return nil
}

func (m *ScheduledMessage) validateTemplate() error {
	if m.Template == nil || m.Template.ID == "" {
		return ErrTemplateRequired
	}
	if m.Text != "" || m.HasMedia() || m.ReplyToMessageID != nil || m.Signed {
		return ErrTemplateWithFreeContent
	}
	if !m.EntryType.SupportsTemplates() {
		return ErrTemplatesUnsupported
	}
	return nil
}

func (m *ScheduledMessage) HasMedia() bool {
	return m.MediaID != nil && *m.MediaID != ""
}

func (m *ScheduledMessage) IsDue(now time.Time) bool {
	return !m.ScheduledAt.After(now)
}

func trimmedOrNil(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
