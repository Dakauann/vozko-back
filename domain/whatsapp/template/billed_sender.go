package template

import (
	"context"
	"errors"
)

var (
	ErrWorkspaceRequired      = errors.New("whatsapp template send: workspace is required, refusing to send unbilled")
	ErrIdempotencyKeyRequired = errors.New("whatsapp template send: idempotency key is required")
	ErrSendInProgress         = errors.New("whatsapp template send: this send is already in progress")
	ErrTemplatePhoneMismatch  = errors.New("whatsapp template send: template does not belong to this number's WhatsApp Business Account")
	ErrPricingUnavailable     = errors.New("whatsapp template send: no price configured for this template category")
	ErrTemplateNotSendable    = errors.New("whatsapp template send: template is not ready to send")
	ErrBillingNotConfigured   = errors.New("whatsapp template send: billing dependencies are not configured")
)

type BilledSendInput struct {
	WorkspaceID    string
	UserID         string
	IdempotencyKey string

	BusinessPhoneID string
	TemplateID      string
	ToNumber        string

	BodyParams   []string
	HeaderParams []string

	CampaignID string
	EntryID    string
}

type BilledSendResult struct {
	AttemptID     string
	Status        SendAttemptStatus
	Outcome       SendOutcome
	MessageID     string
	ChargedMicros int64
	Template      *Template
	Replayed      bool
}

type BilledTemplateSendUseCase interface {
	Execute(ctx context.Context, in BilledSendInput) (*BilledSendResult, error)
}

type TemplateCostReader interface {
	GetTemplateCostMicros(workspaceID string, templateCategory string) (int64, error)
}

type ReconcileSendAttemptsUseCase interface {
	Execute(ctx context.Context) (reconciled int, err error)
}
