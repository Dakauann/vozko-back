package template

import (
	"context"
	"errors"
)

// Errors an operator or a caller can act on, kept apart from infrastructure
// failures so the UI can say what to do rather than "something went wrong".
var (
	// ErrWorkspaceRequired refuses a send that nobody would be billed for.
	//
	// This used to be the opposite: an empty workspace skipped the entire billing
	// block and the message went out free. Making it an error is the whole point
	// of the type — there is no such thing as an unbilled paid template.
	ErrWorkspaceRequired = errors.New("whatsapp template send: workspace is required, refusing to send unbilled")
	// ErrIdempotencyKeyRequired refuses a send that could not be deduplicated.
	ErrIdempotencyKeyRequired = errors.New("whatsapp template send: idempotency key is required")
	// ErrSendInProgress is a replay arriving while the first attempt is still in
	// flight. Deliberately not "retry": we cannot know whether the provider call
	// left the process, so resending risks a second delivered message.
	ErrSendInProgress = errors.New("whatsapp template send: this send is already in progress")
	// ErrTemplatePhoneMismatch is a template belonging to a different WhatsApp
	// Business Account than the number sending it.
	ErrTemplatePhoneMismatch = errors.New("whatsapp template send: template does not belong to this number's WhatsApp Business Account")
	// ErrPricingUnavailable is a workspace with no usable price for this
	// category. Fail closed: the alternative is sending for free.
	ErrPricingUnavailable = errors.New("whatsapp template send: no price configured for this template category")
	// ErrTemplateNotSendable is an approved-but-unusable template, e.g. a media
	// header with no uploaded media.
	ErrTemplateNotSendable = errors.New("whatsapp template send: template is not ready to send")
	// ErrBillingNotConfigured is a mis-wired container. It exists so the failure
	// is a refusal rather than a free send.
	ErrBillingNotConfigured = errors.New("whatsapp template send: billing dependencies are not configured")
)

// BilledSendInput is one paid template send.
//
// Every field that billing depends on is required and unexported-by-convention
// nowhere: the type is the contract that the old code expressed as an `if`.
type BilledSendInput struct {
	// WorkspaceID is who pays. Empty is an error, never a free send.
	WorkspaceID string
	// UserID is who spent it, carried into the attempt row for attribution.
	UserID string
	// IdempotencyKey makes a retry the same send. HTTP callers pass the client's
	// header; the campaign consumer passes its entry id; a workflow passes
	// run+node. Same key, same money.
	IdempotencyKey string

	BusinessPhoneID string
	// TemplateID, never a name. Name lookups resolve through an unordered query
	// that can pick a different row than the operator saw in the picker.
	TemplateID string
	ToNumber   string

	BodyParams   []string
	HeaderParams []string

	// CampaignID and EntryID attach the send to its container so charges stay
	// attributable in department-filtered reporting.
	CampaignID string
	EntryID    string
}

// BilledSendResult is what happened, including for a replay that did nothing.
type BilledSendResult struct {
	AttemptID     string
	Status        SendAttemptStatus
	Outcome       SendOutcome
	MessageID     string
	ChargedMicros int64
	Template      *Template
	// Replayed is true when this call spent nothing because an earlier call with
	// the same key already did. The caller should present the earlier result, not
	// a second success.
	Replayed bool
}

// BilledTemplateSendUseCase is the ONE paid template sender.
//
// It exists because there were four, each with its own billing posture: one that
// skipped billing when a field was empty, one that failed open on a nil
// dependency, one hardened correctly but inlined inside a queue callback where
// nothing else could reach it, and one in between. A fifth caller would have
// meant a fifth posture.
//
// Context comes first, unlike the older port in this package, because everything
// it does is I/O with a deadline that matters.
type BilledTemplateSendUseCase interface {
	Execute(ctx context.Context, in BilledSendInput) (*BilledSendResult, error)
}

// TemplateCostReader prices a category for a workspace. Narrow port so the core
// does not import the balance use-case package wholesale.
type TemplateCostReader interface {
	GetTemplateCostMicros(workspaceID string, templateCategory string) (int64, error)
}

// ReconcileSendAttemptsUseCase settles attempts that took money and never
// reached a terminal state.
//
// It is the backstop for the two crashes the exactly-once argument cannot rule
// out: one between the debit and the provider call, and one between the
// provider's answer and our recording it. Neither can be eliminated without a
// transactional outbox, so instead they are bounded — money is held for at most
// the reconcile window, then returned.
type ReconcileSendAttemptsUseCase interface {
	Execute(ctx context.Context) (reconciled int, err error)
}
