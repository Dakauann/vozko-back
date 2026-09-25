package whatsapp_outreach

import "context"

type StartConversationInput struct {
	WorkspaceID string
	UserID      string
	IsAdmin     bool

	BusinessPhoneID string
	TemplateID      string
	PhoneNumber     string
	Name            string

	BodyParams   []string
	HeaderParams []string

	IdempotencyKey string

	DepartmentIDs []string
}

type StartedConversation struct {
	EntryID   string
	EntryType string

	LeadID    string
	AttemptID string
	MessageID string

	ConversationExisted bool
	Replayed            bool
	ChargedMicros       int64
	Recorded            bool
}

type StartOfficialConversationUseCase interface {
	Execute(ctx context.Context, in StartConversationInput) (*StartedConversation, error)
}

type SendQuote struct {
	Category      string `json:"category"`
	PriceMicros   int64  `json:"priceMicros"`
	BalanceMicros int64  `json:"balanceMicros"`
	Affordable    bool   `json:"affordable"`
}

type QuoteTemplateSendUseCase interface {
	Execute(ctx context.Context, workspaceID, templateID, businessPhoneID string) (*SendQuote, error)
}

type ConversationTemplateInput struct {
	WorkspaceID string
	UserID      string
	EntryID     string

	TemplateID   string
	BodyParams   []string
	HeaderParams []string

	IdempotencyKey string
}

type ConversationTemplate struct {
	Name    string
	Preview string
}

type SentConversationTemplate struct {
	AttemptID     string
	MessageID     string
	ChargedMicros int64
	Replayed      bool
	Recorded      bool
}

type ConversationTemplateUseCase interface {
	Check(ctx context.Context, in ConversationTemplateInput) (*ConversationTemplate, error)
	Send(ctx context.Context, in ConversationTemplateInput) (*SentConversationTemplate, error)
}
