package audience

import (
	"context"
	"time"

	"vozko/domain/shared"
)

// Narrow outbound ports. Each is the smallest interface the engine needs
// from something outside it, so a channel, a provider or Redis can be swapped
// or faked without the engine noticing. It is the posture CommentRuleEvaluator
// and AnalysisSubjectResolver already take.

// ---- Inbound: what a channel calls ----

// IngestInput is what a channel knows about one comment.
type IngestInput struct {
	WorkspaceID      string
	Container        ContainerRef
	SubjectID        string
	ParentSubjectID  string
	AuthorExternalID string
	AuthorHandle     string
	Text             string
	Revision         string
	MessageCount     int
	// OccurredAt is the channel's timestamp; zero means unknown.
	OccurredAt time.Time
	// IsOurs marks the account's own comment. Our replies arrive back as
	// webhooks, and analysing our own copy would poison every aggregate.
	IsOurs bool
}

// Ingestor is what a channel calls for each mirrored comment. Best effort:
// the channel logs the error and moves on, because failing an inbound
// webhook over a queue hiccup would redeliver a message that was already
// stored.
// BacklogReader is how much of a workspace is queued and not yet classified,
// pending and in-flight alike.
//
// It is the consequence half of the volume budget. A ceiling on its own says
// nothing about whether it was set too low; what answers that is whether work
// is stacking up behind it. Deliberately not part of Repository: one use case
// asks this, and widening the store's contract for it would make every other
// caller carry a method it has no use for.
//
// Pending and in-flight count together. To an operator asking "is my ceiling
// holding things up", a row that was claimed but not yet answered is still
// waiting, and splitting the two would only raise the question of which number
// to believe.
type BacklogReader interface {
	CountWaiting(ctx context.Context, workspaceID string) (int, error)
}

type Ingestor interface {
	Enqueue(ctx context.Context, in IngestInput) error
}

// ConversationAnalysisObserver publishes timeline events after persistence.
type ConversationAnalysisObserver interface {
	AnalysisCreated(workspaceID, entryID, entryType, analysisID, disposition string, quality int)
}

// ConversationAnalysisState is what the people looking at ONE conversation are
// told when its analysis moves: it was queued, or it finished.
//
// Pending is its own field rather than an absent Analysis, because the two are
// independent. A conversation being re-analysed is pending AND still carries
// the verdict from its previous revision, and a screen that dropped the old one
// while the new was computed would blink empty every time someone replied.
type ConversationAnalysisState struct {
	EntryID   string    `json:"entryId"`
	EntryType string    `json:"entryType"`
	Pending   bool      `json:"pending"`
	Analysis  *Analysis `json:"analysis,omitempty"`
}

// ConversationAnalysisLive pushes that state to the people watching the
// conversation.
//
// Separate from AnalysisBroadcaster on purpose, and not a second copy of it:
// they answer to different permissions. The audience feed is workspace-wide and
// gated on audience:read; this is one conversation's own state, seen by whoever
// can already open that conversation. Sending it on the audience channel would
// hide it from the inbox, which is the one screen that needs it.
//
// Best effort by contract, like every broadcaster here: it must never return an
// error and never block, because the row is already stored.
type ConversationAnalysisLive interface {
	AnalysisStateChanged(state ConversationAnalysisState)
}

// ---- Outbound: what the engine asks of a channel ----

// ContainerContext is what the prompt says about the post: the model needs
// the caption to tell a critic from a supporter.
type ContainerContext struct {
	Caption   string
	Permalink string
	// AccountName is the account's public handle. Carried here because an
	// alert has to name the account to a human, and the engine only knows it
	// by an internal id.
	AccountName string
	PublishedAt *time.Time
}

// SourceAdapter is a channel's side of the engine, registered per Source.
// The engine never stores comment bodies (§5.1); it reads them back from the
// channel's own table at classification time.
type SourceAdapter interface {
	// ReadTexts returns the text of each comment by source id. Missing ids
	// (deleted since ingest) are simply absent from the map.
	ReadTexts(ctx context.Context, ref ContainerRef, sourceCommentIDs []string) (map[string]string, error)
	ReadContainerContext(ctx context.Context, ref ContainerRef) (ContainerContext, error)
	// ListContainers enumerates an account's posts for a backfill, from the
	// channel's local projection (no provider calls).
	ListContainers(ctx context.Context, accountID string, limit, offset int) ([]ContainerSummary, error)
	// FetchCommentsPage pulls one page of a container's comments off the
	// provider for a backfill. The returned cursor is opaque; empty means
	// the edge is exhausted.
	FetchCommentsPage(ctx context.Context, ref ContainerRef, cursor string) (items []IngestInput, nextCursor string, err error)
}

// ContainerSummary is one post as the backfill sees it.
type ContainerSummary struct {
	Ref           ContainerRef
	WorkspaceID   string
	CommentsCount int
}

// ---- Classification ----

// ClassifyRequest is one batch to send.
type ClassifyRequest struct {
	WorkspaceID string
	// SubjectKind decides which taxonomy the model is offered. A container of
	// conversations must never be sent the comment rubric, and the schema is
	// built from this rather than inferred from the items.
	SubjectKind SubjectKind
	Model       string
	Topics      TopicSet
	Context     ContainerContext
	// Instructions is the operator's context (account and post), already
	// resolved; empty when none was written.
	Instructions string
	Batch        BatchPlan
}

// ClassifyResult is what came back, raw. The use case reconciles refs and
// validates labels; the classifier only transports.
type ClassifyResult struct {
	Results          []BatchResult
	FinishReason     string
	Model            string
	RequestID        string
	PromptTokens     int
	CompletionTokens int
}

// Classifier runs one batch against the model. Implemented over ai.Service;
// a fake stands in for it in every engine test.
type Classifier interface {
	Classify(ctx context.Context, req ClassifyRequest) (*ClassifyResult, error)
}

// ---- Debounce hints ----

// Scheduler keeps the debounce hints (§6.2). Losing all of them costs one
// backstop interval of latency and never a comment.
type Scheduler interface {
	Stamp(ctx context.Context, ref ContainerRef, workspaceID string, now time.Time) error
	Hints(ctx context.Context) ([]Hint, error)
	Clear(ctx context.Context, ref ContainerRef) error
}

// ---- Billing ----

// Charger is the engine's side of billing (§9). Token billing needs none
// of this: setting WorkspaceID on the AI call is the whole integration.
//
// It covers the optional per-batch surcharge and nothing else. The volume
// ceiling used to live here too, as a counter keyed on the UTC calendar date;
// it is now UsageLimiter, which is a different concern with a different
// lifetime and had no business sharing an interface with money.
type Charger interface {
	// ChargeBatch debits the per-batch surcharge, idempotent on batchID.
	// A configured price of 0 is "token billing only", not an error.
	ChargeBatch(ctx context.Context, workspaceID, batchID string, items int) (priceMicros int64, err error)
}

// Clock is the source of "now" (domain/shared/clock.go), injected so the
// debounce and lease arithmetic is testable without sleeping.
type Clock = shared.Clock

// ---- Settings resolution ----

// SettingsResolver answers "what settings apply to this post": the account's
// with the post's override on top, or the disabled defaults for an account
// nobody configured. It never returns ErrNotFound; an unknown account simply
// resolves to "off". The engine and the ingest path depend on this, not on
// the repository, so the fallback rule lives in exactly one place.
type SettingsResolver interface {
	Resolve(ctx context.Context, ref ContainerRef) (*Settings, error)
}

// ---- Escalation (§3) ----

// EscalationDelivery is one message to hand to whichever channel the workspace
// escalates on. The engine formats the text and names a recipient; it does not
// know whether that recipient is a CRM contact, a saved number or a colleague,
// and it never learns which channel carried it.
type EscalationDelivery struct {
	WorkspaceID string
	// RecipientID and RecipientKind are opaque to the engine: they mean
	// whatever the bound sender says they mean, and the sender is the only
	// thing that resolves them. Two fields rather than one composite string,
	// so nothing between here and the channel has to parse anything.
	RecipientID   string
	RecipientKind string
	// ActorUserID is who pressed the button. An outbound message in the
	// workspace's name has an author, and the channel records it.
	ActorUserID string
	Text        string
}

// EscalationSender delivers an escalation. The composition root binds it to
// the channel the workspace actually has, the same way the pipeline seeder
// bridges two aggregates without either knowing the other.
type EscalationSender interface {
	Send(ctx context.Context, in EscalationDelivery) error
}

// ---- Replying (§6) ----

// ReplyDraftRequest is everything the model needs to answer one comment. It
// carries the account's own context rather than a second prompt stack: the
// same instructions and the same post caption the classifier already reads.
type ReplyDraftRequest struct {
	WorkspaceID  string
	Model        string
	Instructions string
	Caption      string

	Comment      string
	AuthorHandle string
	Intent       Intent
	Stance       Stance
	Sentiment    string
	Language     string
	// MaxLength is the domain's bound, passed down so the model is asked for
	// something the domain will not then truncate mid-sentence.
	MaxLength int
}

type ReplyDraftResult struct {
	Text             string
	Model            string
	RequestID        string
	PromptTokens     int
	CompletionTokens int
}

// ReplyDrafter writes one draft. Implemented over ai.Service, the same posture
// Classifier takes, so a fake stands in for it in every test.
type ReplyDrafter interface {
	Draft(ctx context.Context, req ReplyDraftRequest) (*ReplyDraftResult, error)
}

// CommentReplier posts a public threaded reply on the channel.
//
// A port rather than a method on SourceAdapter: replying is a channel's
// existing capability with its own use case, permissions and side effects, and
// the composition root binds this to it. The engine only wants "put these words
// under that comment".
type CommentReplier interface {
	ReplyToComment(ctx context.Context, workspaceID, accountID, sourceCommentID, text string) (replyID string, err error)
}

// ---- Live feed (§7) ----

// AnalysisBroadcaster publishes a batch of freshly classified comments.
//
// Best effort by contract: the implementation must never return an error and
// must never block the caller, because a socket having a bad day must not fail
// an analysis that was already paid for and stored. Same posture the contact
// and lead bridges take today.
type AnalysisBroadcaster interface {
	BroadcastCommentsAnalyzed(event AnalysisBatchAnalyzed)
}

// ---- Author role inference (§5) ----

// RoleInferRequest is one author's corpus, for one pass.
//
// The comments travel as plain strings and nothing else: the model is being
// asked what this person's public role appears to be, and a handle, a follower
// count or a stance label would only invite it to answer from a stereotype
// instead of from the words.
type RoleInferRequest struct {
	WorkspaceID string
	Model       string
	// Instructions is the account's own context, the same text the classifier
	// reads: "who this account is" changes what a mention of the city council
	// implies about the person mentioning it.
	Instructions string
	Comments     []string
}

type RoleInferResult struct {
	Role       AuthorRole
	Confidence string
	Rationale  string

	Model            string
	PromptTokens     int
	CompletionTokens int
}

// RoleInferrer runs one author pass. Implemented over ai.Service beside the
// classifier, and faked in every test.
type RoleInferrer interface {
	InferRole(ctx context.Context, req RoleInferRequest) (*RoleInferResult, error)
}

// ---- Alerts ----

// AlertDelivery is one alert handed to whichever channel the rule names. The
// engine formats both forms; it does not know how either is sent.
type AlertDelivery struct {
	WorkspaceID string
	Channel     AlertChannel
	Recipient   string

	// Official: which number it leaves from and which approved template.
	BusinessPhoneID string
	TemplateID      string
	// TemplateParams is the canonical fill, for a dispatcher that cannot
	// resolve the template. Facts is the same content keyed by meaning, so a
	// dispatcher that CAN resolve it fills the template's own shape, named or
	// positional, however many variables it declares.
	TemplateParams []string
	Facts          []AlertFact

	// Unofficial: which connected number, empty meaning "pick one", and the
	// free text.
	InstanceID string
	Text       string

	// ActorUserID is who armed the rule, and therefore who its messages are
	// attributed to. A paid automated send with no author is a support ticket
	// nobody can answer.
	ActorUserID string

	// IdempotencyKey identifies this firing. A dispatcher that retries must
	// pass it through, so the same firing cannot arrive twice.
	IdempotencyKey string
}

// AlertDispatcher sends one alert. The composition root binds it to the two
// existing outbound use cases; nothing here knows about either.
type AlertDispatcher interface {
	Dispatch(ctx context.Context, in AlertDelivery) error
}

// AlertEvaluator is what the engine calls after a batch is stored.
//
// Best effort by contract: it returns nothing and must never block, because an
// alert is a nicety and the classification it rides on has already been paid
// for and written. Same posture as AnalysisBroadcaster.
type AlertEvaluator interface {
	EvaluateBatch(ctx context.Context, ref ContainerRef, workspaceID string, rows []*Analysis)
}

// AlertBriefRequest is one alert, for the model to read.
//
// It carries what already exists: the comment, the post's caption, and the
// account's own instructions. Nothing is fetched for it.
type AlertBriefRequest struct {
	WorkspaceID  string
	Model        string
	Instructions string
	Caption      string

	RuleName    string
	Measurement string
	Comment     string
	Stance      Stance
	Severity    int
}

// AlertBriefer writes the model's reading of an alert: why it matters and how
// to respond.
//
// Best effort by contract, and the caller must send the alert without it on any
// error. An alert fires exactly when something is going wrong, which is the
// worst moment to make delivery depend on a model call.
type AlertBriefer interface {
	Brief(ctx context.Context, req AlertBriefRequest) (*AlertBriefing, error)
}
