// Package whatsapp_outreach is cold outbound on the OFFICIAL WhatsApp channel:
// reaching a number that never wrote to us.
//
// It is a separate package from whatsapp_campaign, which owns bulk sending, and
// from whatsapp/template, which owns what a template is and what sending one
// costs. This package owns only the composition: who may reach whom, from which
// number, and what has to be true first.
//
// The unofficial channel's equivalent (usecases/unofficial_whatsapp) stops at
// "the conversation exists" and leaves sending to the composer. This one cannot:
// on the official channel a stranger has no open window, so the composer is
// blocked and the only legal first message is a paid template. Sending IS the
// use case here, which is why every guard below runs before the money moves.
package whatsapp_outreach

import "context"

// StartConversationInput is one operator reaching one number.
type StartConversationInput struct {
	WorkspaceID string
	// UserID is the operator. Carried all the way to the message row and the
	// send attempt, because a charge nobody can be attributed to is a support
	// ticket nobody can answer.
	UserID string
	// IsAdmin relaxes department scoping the same way it does everywhere else.
	IsAdmin bool

	BusinessPhoneID string
	// TemplateID, never a template name: a name resolves through an unordered
	// query that can pick a different row than the one the operator chose.
	TemplateID string
	// PhoneNumber as typed. Normalised inside the use case so every caller gets
	// the same treatment and a value that cannot be normalised is refused before
	// anything is spent.
	PhoneNumber string
	// Name is optional and only used when the lead is new.
	Name string

	BodyParams   []string
	HeaderParams []string

	// IdempotencyKey makes a retry the same send. Required — a paid action with
	// no key is a double charge waiting for a flaky connection.
	IdempotencyKey string

	// DepartmentIDs is the caller's department scope, empty for unrestricted.
	DepartmentIDs []string
}

// StartedConversation is where to take the operator, and what it cost.
type StartedConversation struct {
	// EntryID and EntryType address the conversation the way the CRM does.
	//
	// On this channel the inbox entry IS the campaign entry, so returning
	// anything else — a campaign id, a lead id — would hand the inbox an id it
	// cannot resolve and drop the operator onto a blank thread.
	EntryID   string
	EntryType string

	LeadID    string
	AttemptID string
	MessageID string

	// ConversationExisted is true when this number was already in the inbox.
	// The operator is taken to the same place either way; the UI can say
	// "opened" rather than implying a duplicate was created.
	ConversationExisted bool
	// Replayed is true when an identical request had already been sent. Nothing
	// was spent and nothing was sent a second time.
	Replayed bool
	// ChargedMicros is what this send cost, echoed back so the UI can show it
	// rather than making the operator find it in the ledger.
	ChargedMicros int64
	// Recorded is false when the message was delivered but could not be written
	// to the thread. The send still succeeded; saying otherwise would invite a
	// retry that charges twice.
	Recorded bool
}

// StartOfficialConversationUseCase opens a conversation by sending a template.
type StartOfficialConversationUseCase interface {
	Execute(ctx context.Context, in StartConversationInput) (*StartedConversation, error)
}

// SendQuote is what a send will cost, answered before the operator commits to it.
//
// It exists because this is the one dialog in the product that spends money on
// submit. An operator who cannot see the price until the ledger moves has been
// asked to consent to something they were not shown.
type SendQuote struct {
	Category      string `json:"category"`
	PriceMicros   int64  `json:"priceMicros"`
	BalanceMicros int64  `json:"balanceMicros"`
	Affordable    bool   `json:"affordable"`
}

// QuoteTemplateSendUseCase prices one send for one workspace.
type QuoteTemplateSendUseCase interface {
	Execute(ctx context.Context, workspaceID, templateID, businessPhoneID string) (*SendQuote, error)
}
