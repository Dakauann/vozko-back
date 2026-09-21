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
