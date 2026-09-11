// Package comment_analysis is the channel-neutral engine that classifies public
// comments on a customer's posts, in bulk, on a token budget.
//
// It is pure: no Instagram, no SQL, no HTTP. A comment arrives as a Source
// plus a ContainerRef (the post it sits under) and leaves as a row of
// constrained labels. Everything derived from those labels (severity, whether
// the comment needs a reply, an author's standing over time, the acceptance
// score) is computed here and never asked of the model, for the reason
// domain/shared/rubric.go gives: models rate ordinal levels consistently and
// invent numbers inconsistently.
//
// A comment is NOT a conversation (§2.1 of the plan). It has no agent to rate,
// no disposition to reach and no transcript, so it does not go into the
// conversation `analyses` table. The rubric machinery is shared; the taxonomy
// is not.
package audience

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/shared"
)

const (
	// ExcerptMaxRunes caps what the dashboard row shows. The full text is never
	// copied (§5.1): it lives in the channel's own table and inherits that
	// table's retention and PII posture. Runes, not bytes: a byte cut lands
	// mid-character in pt-BR.
	ExcerptMaxRunes = 200

	// MaxAttempts bounds retries per row. A comment the model keeps dropping is
	// marked failed and shown, never dropped silently and never looped forever.
	MaxAttempts = 3

	// DefaultActionThreshold is the severity at which a comment needs a human
	// regardless of intent.
	DefaultActionThreshold = 60

	// MaxSummaryRunes caps the conversation summary. It is the only free-text
	// field the engine stores and the only one carrying unredacted customer
	// content, so it is bounded here rather than trusted from the model: an
	// unbounded summary is an unbounded PII surface and an unbounded row.
	MaxSummaryRunes = 1200

	// MaxProductInterestRunes caps the free-text product note. Short on
	// purpose: it is a label, not a paragraph, and the legacy engine's
	// unbounded version is why it could never be counted or grouped.
	MaxProductInterestRunes = 120
)

// Machine-generated failure reasons. FailureReason itself is free text so a
// provider's own message can be kept verbatim; these name the cases the UI
// renders specially.
const (
	ReasonMissingRef          = "missing_ref"
	ReasonInvalidLabels       = "invalid_labels"
	ReasonUnparseableResponse = "unparseable_response"
	ReasonResponseTruncated   = "response_truncated"
	ReasonProviderError       = "provider_error"
	ReasonDispatchInterrupted = "dispatch_interrupted"
	ReasonTextUnavailable     = "text_unavailable"
	ReasonAnalysisDisabled    = "analysis_disabled"
)

// ---- Source and container ----

// Source names the channel a subject came from.
//
// It is the same vocabulary as shared.EntryType and deliberately NOT an alias
// of it. An alias would drag EntryType.Valid() along, and that predicate
// answers a different question, "is this a messaging channel", which admits
// support (never analysed) and rejects voice (the richest transcripts we
// have). Every Source.Valid() call site in this engine means "can we analyse
// this channel", so that is what it delegates to. The channel sets themselves
// live once, in domain/shared/entry_type.go.
type Source string

const (
	SourceInstagram          Source = Source(shared.EntryTypeInstagram)
	SourceWhatsApp           Source = Source(shared.EntryTypeWhatsApp)
	SourceTelegram           Source = Source(shared.EntryTypeTelegram)
	SourceUnofficialWhatsApp Source = Source(shared.EntryTypeUnofficialWhatsApp)
	SourceVoice              Source = Source(shared.EntryTypeVoice)
)

// EntryType converts to the shared channel vocabulary, for the ports that key
// on it (conversation transcripts, message history).
func (s Source) EntryType() shared.EntryType { return shared.EntryType(s) }

// SourceOf is the inverse, for callers holding a shared.EntryType.
func SourceOf(e shared.EntryType) Source { return Source(e) }

// Valid reports whether the engine can analyse anything at all on this
// channel. Which SUBJECT is analysable is a narrower question, answered by
// SubjectKind.SupportedOn.
func (s Source) Valid() bool {
	return s.EntryType().SupportsAnalysis()
}

// ---- Subject kind ----

// SubjectKind is what a row is about. The engine analyses two things and the
// difference is not cosmetic: a comment is one utterance by a stranger under a
// post, a conversation is a two-sided exchange with an agent to rate and an
// objective to reach. They carry different labels (see Classification) and
// come from different channels.
type SubjectKind string

const (
	SubjectKindComment      SubjectKind = "comment"
	SubjectKindConversation SubjectKind = "conversation"
)

func (k SubjectKind) Valid() bool {
	switch k {
	case SubjectKindComment, SubjectKindConversation:
		return true
	}
	return false
}

func SubjectKindValues() []string {
	return []string{string(SubjectKindComment), string(SubjectKindConversation)}
}

// SupportedOn reports whether this kind of subject exists on this channel.
// Telegram has no public posts to comment under; voice has no comments but the
// longest transcripts. Both questions are answered by the channel sets in
// domain/shared, so adding a channel is still one edit there.
func (k SubjectKind) SupportedOn(s Source) bool {
	switch k {
	case SubjectKindComment:
		return s.EntryType().SupportsCommentAnalysis()
	case SubjectKindConversation:
		return s.EntryType().SupportsConversationAnalysis()
	}
	return false
}

// ContainerRef identifies the thing comments sit under (for Instagram, a
// post). Debounce, locking and the backstop sweep are all keyed on it, because
// a post taking 5,000 comments in ten minutes must produce ONE unit of work,
// not 5,000.
type ContainerRef struct {
	// Kind is what the container holds. The zero value reads as a comment, so
	// every existing construction site and every key already sitting in Redis
	// keeps its meaning.
	Kind        SubjectKind
	Source      Source
	AccountID   string
	ContainerID string
}

// withDefaults makes the implicit comment kind explicit. Reading it in one
// place keeps the "empty means comment" rule from being restated in Key,
// Validate and every caller.
func (r ContainerRef) withDefaults() ContainerRef {
	if r.Kind == "" {
		r.Kind = SubjectKindComment
	}
	return r
}

// Key renders the ref as the Redis hash field and the lock name. Pinned
// format; ParseContainerKey is its inverse.
//
// A comment key keeps the original three-segment shape so the debounce entries
// already in Redis survive this change. A conversation key is prefixed with its
// kind, which also stops a post id and a campaign id from ever colliding on the
// same lock.
func (r ContainerRef) Key() string {
	r = r.withDefaults()
	base := string(r.Source) + ":" + r.AccountID + ":" + r.ContainerID
	if r.Kind == SubjectKindComment {
		return base
	}
	return string(r.Kind) + ":" + base
}

// Normalized returns the ref with the implicit comment kind made explicit.
//
// Infrastructure needs this: subject_kind is a NOT NULL column, so writing or
// querying the zero value would look for an empty string where 'comment' is
// meant, and find nothing.
func (r ContainerRef) Normalized() ContainerRef { return r.withDefaults() }

// Equal compares two refs by meaning rather than by struct identity.
//
// Use it instead of ==. A ref built literally without a Kind means the same
// container as one built with SubjectKindComment, but Go's == says they differ,
// and that difference is invisible at the call site: a lookup simply returns
// nothing.
func (r ContainerRef) Equal(o ContainerRef) bool {
	return r.withDefaults() == o.withDefaults()
}

func (r ContainerRef) Validate() error {
	r = r.withDefaults()
	if !r.Kind.Valid() || r.AccountID == "" || r.ContainerID == "" {
		return ErrContainerInvalid
	}
	// The narrow question, not Source.Valid(): a container of comments on
	// Telegram is not a thing, however analysable Telegram is.
	if !r.Kind.SupportedOn(r.Source) {
		return ErrContainerInvalid
	}
	return nil
}

// ParseContainerKey is the inverse of Key. A key whose first segment names a
// subject kind is the prefixed form; anything else is the original comment
// form.
func ParseContainerKey(key string) (ContainerRef, error) {
	kind := SubjectKindComment
	if head, rest, ok := strings.Cut(key, ":"); ok && SubjectKind(head).Valid() {
		kind, key = SubjectKind(head), rest
	}
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 {
		return ContainerRef{}, fmt.Errorf("%w: %q", ErrContainerInvalid, key)
	}
	r := ContainerRef{Kind: kind, Source: Source(parts[0]), AccountID: parts[1], ContainerID: parts[2]}
	if err := r.Validate(); err != nil {
		return ContainerRef{}, fmt.Errorf("%w: %q", err, key)
	}
	return r, nil
}

// ---- Status machine ----

// Status is where a row is in its life.
//
//	pending ──claim──▶ in_flight ──apply──▶ analyzed
//	   │                  ├──release──▶ pending   (missing ref, crash reset)
//	   │                  └──────────▶ failed    (max attempts, provider error)
//	   ├──────────────────────────────▶ failed
//	   └──────────────────────────────▶ skipped   (nothing to classify)
//	failed ──retry──▶ pending
//
// Every move out of pending is a conditional write guarded on the current
// status (the repository's ClaimPending). That guard, not a lock, is what
// keeps two ticks from sending the same comment to the model twice.
type Status string

const (
	StatusPending  Status = "pending"
	StatusInFlight Status = "in_flight"
	StatusAnalyzed Status = "analyzed"
	StatusFailed   Status = "failed"
	StatusSkipped  Status = "skipped"
)

func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusInFlight, StatusAnalyzed, StatusFailed, StatusSkipped:
		return true
	}
	return false
}

// IsTerminal reports whether nothing further will happen to this row. failed
// is deliberately NOT terminal: the retry endpoint re-queues it.
func (s Status) IsTerminal() bool {
	return s == StatusAnalyzed || s == StatusSkipped
}

// CanTransitionTo is exhaustive rather than "anything but terminal", for the
// same reason the scheduled-message machine is: a row that could go from
// analyzed back to pending would be billed twice.
func (s Status) CanTransitionTo(next Status) bool {
	switch s {
	case StatusPending:
		return next == StatusInFlight || next == StatusFailed || next == StatusSkipped
	case StatusInFlight:
		return next == StatusAnalyzed || next == StatusPending || next == StatusFailed
	case StatusFailed:
		return next == StatusPending
	default:
		return false
	}
}

// ---- Labels ----

// Stance is the commenter's position toward the subject of the post. critic
// disagrees with the subject; hostile attacks the person. The deck's
// Simpatizante / Neutro / Crítico / Hater.
type Stance string

const (
	StanceSupporter Stance = "supporter"
	StanceNeutral   Stance = "neutral"
	StanceCritic    Stance = "critic"
	StanceHostile   Stance = "hostile"
)

func (s Stance) Valid() bool {
	switch s {
	case StanceSupporter, StanceNeutral, StanceCritic, StanceHostile:
		return true
	}
	return false
}

func StanceValues() []string {
	return []string{string(StanceSupporter), string(StanceNeutral), string(StanceCritic), string(StanceHostile)}
}

// Intent is what the commenter wants. It drives the recommended action.
type Intent string

const (
	IntentPraise         Intent = "praise"
	IntentQuestion       Intent = "question"
	IntentComplaint      Intent = "complaint"
	IntentSupportRequest Intent = "support_request"
	IntentSpam           Intent = "spam"
	IntentSalesLead      Intent = "sales_lead"
	IntentOther          Intent = "other"
)

func (i Intent) Valid() bool {
	switch i {
	case IntentPraise, IntentQuestion, IntentComplaint, IntentSupportRequest, IntentSpam, IntentSalesLead, IntentOther:
		return true
	}
	return false
}

func IntentValues() []string {
	return []string{
		string(IntentPraise), string(IntentQuestion), string(IntentComplaint), string(IntentSupportRequest),
		string(IntentSpam), string(IntentSalesLead), string(IntentOther),
	}
}

// ---- The per-comment record ----

// Analysis is one classified comment.
type Analysis struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`

	// SubjectKind says what this row is about. Empty reads as a comment, so
	// every row written before conversations existed keeps its meaning without
	// a backfill.
	SubjectKind SubjectKind `json:"subjectKind,omitempty"`

	Source      Source `json:"source"`
	AccountID   string `json:"accountId"`
	ContainerID string `json:"containerId"`
	// SubjectID identifies the subject on its channel: the comment id for
	// a comment, the conversation's entry id for a conversation.
	SubjectID string  `json:"subjectId"`
	ParentSubjectID *string `json:"parentCommentId,omitempty"`

	AuthorExternalID string `json:"authorExternalId"`
	AuthorHandle     string `json:"authorHandle,omitempty"`

	Status        Status `json:"status"`
	Attempts      int    `json:"attempts"`
	FailureReason string `json:"failureReason,omitempty"`

	// Model output, all constrained by the rubric.
	Sentiment shared.Sentiment `json:"sentiment,omitempty"`
	Stance    Stance           `json:"stance,omitempty"`
	Intent    Intent           `json:"intent,omitempty"`
	TopicKey  string           `json:"topicKey,omitempty"`
	IsSpam    bool             `json:"isSpam"`
	Language  string           `json:"language,omitempty"`

	// Severity dimensions: rated ordinally by the model, scored here.
	Toxicity       shared.QualityLevel `json:"toxicity,omitempty"`
	PersonalAttack shared.QualityLevel `json:"personalAttack,omitempty"`
	LegalRisk      shared.QualityLevel `json:"legalRisk,omitempty"`
	// Severity is 0-100, COMPUTED. Never model-set.
	Severity int `json:"severity"`

	// ---- Conversation labels ----
	//
	// Set only when SubjectKind is conversation, and zero for every comment
	// row. They are the taxonomy the legacy conversation engine owned; see
	// conversation.go. Kept flat rather than behind a pointer struct because
	// they are filtered and aggregated in SQL, and a nested value would have to
	// be unpacked in every query.
	Interest        Interest      `json:"interest,omitempty"`
	ProductInterest string        `json:"productInterest,omitempty"`
	Disposition     Disposition   `json:"disposition,omitempty"`
	Qualification   Qualification `json:"qualification,omitempty"`
	NextAction      NextAction    `json:"nextAction,omitempty"`
	// Summary is the model's prose. It is the one free-text field the engine
	// stores and the only one carrying unredacted customer content, which is
	// why retention applies to it like everything else here.
	Summary string `json:"summary,omitempty"`
	// AttendanceQuality is 0-100, COMPUTED from the conversation quality
	// rubric's ordinal levels. Never model-set, exactly as Severity is not.
	AttendanceQuality int `json:"attendanceQuality,omitempty"`
	MessageCount      int `json:"messageCount,omitempty"`

	// RequiresAction is DERIVED from severity and intent; see ActionPolicy.
	RequiresAction bool   `json:"requiresAction"`
	Excerpt        string `json:"excerpt"`
	// Truncated records that the text was cut before it reached the model or
	// the excerpt, so a classification of a partial comment is never silent.
	Truncated bool `json:"truncated"`

	BatchID    string     `json:"batchId,omitempty"`
	Model      string     `json:"model,omitempty"`
	AnalyzedAt *time.Time `json:"analyzedAt,omitempty"`
	// OccurredAt is when the comment was POSTED on the channel; CreatedAt
	// is when it reached us. Rollups bucket by the former, so a backfilled
	// comment lands on its own day.
	OccurredAt time.Time `json:"occurredAt"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// DeletedAt is set when the source comment was deleted (§6.4). The row
	// leaves the feed and the live stats but stays in historical rollups: the
	// rollup for last Tuesday must not change because someone deleted a
	// comment today.
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}

// SoftDelete tombstones the row. Idempotent.
func (a *Analysis) SoftDelete(now time.Time) {
	if a.DeletedAt != nil {
		return
	}
	a.DeletedAt = &now
	a.UpdatedAt = now
}

// NewInput is what ingest knows about a comment. The ID is assigned by the
// caller (the use case), as everywhere else in the tree.
type NewInput struct {
	WorkspaceID      string
	Container        ContainerRef
	SubjectID  string
	ParentSubjectID  string
	AuthorExternalID string
	AuthorHandle     string
	Text             string
	// OccurredAt is the channel's timestamp for the comment; zero falls
	// back to Now.
	OccurredAt time.Time
	Now         time.Time
}

// NewPending builds the row ingest inserts. A blank comment is recorded as
// skipped rather than pending: the totals stay honest and the model is never
// sent nothing to classify.
func NewPending(in NewInput) (*Analysis, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, ErrWorkspaceRequired
	}
	if strings.TrimSpace(in.SubjectID) == "" {
		return nil, ErrSubjectIDRequired
	}
	if err := in.Container.Validate(); err != nil {
		return nil, err
	}

	container := in.Container.withDefaults()
	excerpt, cut := Excerpt(in.Text)
	a := &Analysis{
		WorkspaceID:      in.WorkspaceID,
		SubjectKind:      container.Kind,
		Source:           in.Container.Source,
		AccountID:        in.Container.AccountID,
		ContainerID:      in.Container.ContainerID,
		SubjectID:  strings.TrimSpace(in.SubjectID),
		AuthorExternalID: strings.TrimSpace(in.AuthorExternalID),
		AuthorHandle:     strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(in.AuthorHandle), "@")),
		Status:           StatusPending,
		Excerpt:          excerpt,
		Truncated:        cut,
		OccurredAt:      in.OccurredAt,
		CreatedAt:        in.Now,
		UpdatedAt:        in.Now,
	}
	if a.OccurredAt.IsZero() {
		a.OccurredAt = in.Now
	}
	if p := strings.TrimSpace(in.ParentSubjectID); p != "" {
		a.ParentSubjectID = &p
	}
	if excerpt == "" {
		a.Status = StatusSkipped
	}
	return a, nil
}

// Excerpt trims and caps text at ExcerptMaxRunes, reporting whether it cut.
func Excerpt(text string) (string, bool) {
	return TruncateRunes(strings.TrimSpace(text), ExcerptMaxRunes)
}

// TruncateRunes cuts s to at most max runes, never splitting a character.
func TruncateRunes(s string, max int) (string, bool) {
	if max <= 0 {
		return "", s != ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s, false
	}
	return string(runes[:max]), true
}

// Container returns the ref this row belongs to.
func (a *Analysis) Container() ContainerRef {
	return ContainerRef{
		Kind: a.SubjectKind, Source: a.Source,
		AccountID: a.AccountID, ContainerID: a.ContainerID,
	}.withDefaults()
}

// Kind is the subject kind with the "empty means comment" rule applied, so
// readers never have to know the rule.
func (a *Analysis) Kind() SubjectKind {
	if a.SubjectKind == "" {
		return SubjectKindComment
	}
	return a.SubjectKind
}

func (a *Analysis) transition(next Status, now time.Time) error {
	if !a.Status.CanTransitionTo(next) {
		return fmt.Errorf("%w: %s -> %s", ErrStatusTransition, a.Status, next)
	}
	a.Status = next
	a.UpdatedAt = now
	return nil
}

// Claim moves the row into a batch. Attempts is counted HERE, before the
// model is called, so a crash between claim and apply still consumes a try
// and a crash loop terminates.
func (a *Analysis) Claim(now time.Time) error {
	if err := a.transition(StatusInFlight, now); err != nil {
		return err
	}
	a.Attempts++
	return nil
}

// Release is the reconcile path for a claimed row the model did not answer
// for (or a stale in_flight row the backstop found). It goes back to pending
// to be retried, until MaxAttempts turns it into a visible failure.
func (a *Analysis) Release(reason string, now time.Time) {
	if a.Status != StatusInFlight {
		return
	}
	if a.Attempts >= MaxAttempts {
		_ = a.transition(StatusFailed, now)
		a.FailureReason = reason
		return
	}
	_ = a.transition(StatusPending, now)
	a.FailureReason = reason
}

// Unclaim hands a claimed row back WITHOUT the attempt. For a provider
// outage: the comment did nothing wrong, and three ticks of outage must not
// turn every pending row into a failure. No-op unless in_flight.
func (a *Analysis) Unclaim(now time.Time) {
	if a.Status != StatusInFlight {
		return
	}
	_ = a.transition(StatusPending, now)
	if a.Attempts > 0 {
		a.Attempts--
	}
}

// MarkSkipped records that there was nothing to classify (the text vanished
// between ingest and flush, or analysis was switched off). Terminal.
func (a *Analysis) MarkSkipped(reason string, now time.Time) error {
	if a.Status == StatusInFlight {
		// A claimed row goes back through pending so the transition table
		// stays the single description of what is legal.
		a.Unclaim(now)
	}
	if err := a.transition(StatusSkipped, now); err != nil {
		return err
	}
	a.FailureReason = reason
	return nil
}

// Fail marks a definitive failure with a reason the UI shows.
func (a *Analysis) Fail(reason string, now time.Time) error {
	if err := a.transition(StatusFailed, now); err != nil {
		return err
	}
	a.FailureReason = reason
	return nil
}

// Retry re-queues a failed row with a fresh set of attempts. Only an operator
// calls this, so giving the row its full allowance again is the intent.
func (a *Analysis) Retry(now time.Time) error {
	if err := a.transition(StatusPending, now); err != nil {
		return err
	}
	a.Attempts = 0
	a.FailureReason = ""
	return nil
}

// Provenance records which call produced a classification.
type Provenance struct {
	BatchID string
	Model   string
}

// Apply writes a validated classification onto a claimed row and derives
// everything the model was not asked for. The caller validates against the
// container's topic set first; Apply trusts its input.
func (a *Analysis) Apply(c Classification, policy ActionPolicy, prov Provenance, now time.Time) error {
	if err := a.transition(StatusAnalyzed, now); err != nil {
		return err
	}
	policy.Normalize()

	if a.Kind() == SubjectKindConversation {
		a.applyConversation(c, prov, now)
		return nil
	}

	a.Sentiment = c.Sentiment
	a.Stance = c.Stance
	a.Intent = c.Intent
	a.TopicKey = c.TopicKey
	a.IsSpam = c.IsSpam
	a.Language = c.Language
	a.Toxicity = c.Toxicity
	a.PersonalAttack = c.PersonalAttack
	a.LegalRisk = c.LegalRisk

	a.Severity = c.Severity()
	a.RequiresAction = policy.RequiresAction(a.Severity, a.Intent)
	a.FailureReason = ""
	a.BatchID = prov.BatchID
	a.Model = prov.Model
	analyzedAt := now
	a.AnalyzedAt = &analyzedAt
	return nil
}

// applyConversation writes the conversation labels. The comment dimensions
// (stance, intent, topic, spam, the three severity ordinals) are deliberately
// left at their zero values: a conversation has no position to take against a
// third party and no post to be on topic about, and writing a computed severity
// of 0 onto it would put every conversation at the bottom of a severity sort as
// though it had been assessed and found harmless.
//
// RequiresAction is derived from the next action instead, which is the
// conversation's equivalent question: escalate means a human is needed.
func (a *Analysis) applyConversation(c Classification, prov Provenance, now time.Time) {
	a.Sentiment = c.Sentiment
	a.Interest = c.Interest
	a.ProductInterest = c.ProductInterest
	a.Disposition = c.Disposition
	a.Qualification = c.Qualification
	a.NextAction = c.NextAction
	a.Summary = c.Summary
	a.Language = c.Language
	a.AttendanceQuality = c.Quality.Score()

	a.RequiresAction = c.NextAction == NextActionEscalate
	a.FailureReason = ""
	a.BatchID = prov.BatchID
	a.Model = prov.Model
	analyzedAt := now
	a.AnalyzedAt = &analyzedAt
}

// ---- Classification: the model's answer for one comment ----

// Classification is exactly the constrained labels the model returns for one
// comment. It carries no number: Severity() computes one.
type Classification struct {
	Sentiment shared.Sentiment
	Stance    Stance
	Intent    Intent
	TopicKey  string
	IsSpam    bool
	Language  string

	Toxicity       shared.QualityLevel
	PersonalAttack shared.QualityLevel
	LegalRisk      shared.QualityLevel

	// ---- Conversation subjects only ----
	//
	// A conversation carries no stance, topic or toxicity, and a comment
	// carries none of these. Both sets live on one struct because one batch
	// decoder fills it; ValidateFor is what refuses a mixture.
	Interest        Interest
	ProductInterest string
	Disposition     Disposition
	Qualification   Qualification
	NextAction      NextAction
	Summary         string
	// Quality is rated ordinally by the model; the 0-100 is computed from it,
	// never asked for.
	Quality ConversationQuality
}

// Validate rejects any label outside the rubric or a topic outside the
// container's set. The topic key is canonicalised in place: a model that
// echoes the label instead of the key still lands on one topic, not two.
func (c *Classification) Validate(topics TopicSet) error {
	if !c.Sentiment.Valid() {
		return fmt.Errorf("%w: sentiment %q", ErrInvalidClassification, c.Sentiment)
	}
	if !c.Stance.Valid() {
		return fmt.Errorf("%w: stance %q", ErrInvalidClassification, c.Stance)
	}
	if !c.Intent.Valid() {
		return fmt.Errorf("%w: intent %q", ErrInvalidClassification, c.Intent)
	}
	key, ok := topics.Resolve(c.TopicKey)
	if !ok {
		return fmt.Errorf("%w: topic %q", ErrInvalidClassification, c.TopicKey)
	}
	c.TopicKey = key
	for _, d := range []struct {
		key string
		lvl shared.QualityLevel
	}{
		{SeverityKeyToxicity, c.Toxicity},
		{SeverityKeyPersonalAttack, c.PersonalAttack},
		{SeverityKeyLegalRisk, c.LegalRisk},
	} {
		if !d.lvl.Valid() {
			return fmt.Errorf("%w: %s %q", ErrInvalidClassification, d.key, d.lvl)
		}
	}
	c.Language = strings.TrimSpace(c.Language)
	return nil
}

// ValidateFor validates against the subject kind. Validate above stays the
// comment path unchanged; this is the entry point the engine uses once a row
// can be either kind.
func (c *Classification) ValidateFor(kind SubjectKind, topics TopicSet) error {
	switch kind {
	case SubjectKindConversation:
		return c.ValidateConversation()
	case SubjectKindComment, "":
		return c.Validate(topics)
	}
	return fmt.Errorf("%w: subject kind %q", ErrInvalidClassification, kind)
}

// ValidateConversation rejects any label outside the conversation rubric.
//
// It deliberately does NOT accept a partially rated quality assessment: a
// missing dimension would otherwise score as "none" and produce a lower number
// that reads like a real assessment rather than a failed one.
func (c *Classification) ValidateConversation() error {
	if !c.Sentiment.Valid() {
		return fmt.Errorf("%w: sentiment %q", ErrInvalidClassification, c.Sentiment)
	}
	if !c.Interest.Valid() {
		return fmt.Errorf("%w: interest %q", ErrInvalidClassification, c.Interest)
	}
	if !c.Disposition.Valid() {
		return fmt.Errorf("%w: disposition %q", ErrInvalidClassification, c.Disposition)
	}
	if !c.Qualification.Valid() {
		return fmt.Errorf("%w: qualification %q", ErrInvalidClassification, c.Qualification)
	}
	if !c.NextAction.Valid() {
		return fmt.Errorf("%w: next action %q", ErrInvalidClassification, c.NextAction)
	}
	if !c.Quality.Valid() {
		return fmt.Errorf("%w: incomplete attendance quality", ErrInvalidClassification)
	}
	c.Summary, _ = TruncateRunes(strings.TrimSpace(c.Summary), MaxSummaryRunes)
	c.ProductInterest, _ = TruncateRunes(strings.TrimSpace(c.ProductInterest), MaxProductInterestRunes)
	c.Language = strings.TrimSpace(c.Language)
	return nil
}

func (c Classification) levelFor(key string) shared.QualityLevel {
	switch key {
	case SeverityKeyToxicity:
		return c.Toxicity
	case SeverityKeyPersonalAttack:
		return c.PersonalAttack
	case SeverityKeyLegalRisk:
		return c.LegalRisk
	}
	return shared.QualityLevelNone
}

// Severity is the 0-100 composed from the three ordinal dimensions with the
// rubric's weights. Always within [0,100].
func (c Classification) Severity() int {
	return shared.WeightedScore(SeverityDimensions(), c.levelFor)
}

// ---- Action policy ----

// ActionPolicy decides when a comment needs a human. The threshold is
// per-account (the settings UI edits it); the intent rule is fixed.
type ActionPolicy struct {
	SeverityThreshold int
}

// Normalize fills the default and clamps to the score's range.
func (p *ActionPolicy) Normalize() {
	if p.SeverityThreshold <= 0 {
		p.SeverityThreshold = DefaultActionThreshold
	}
	if p.SeverityThreshold > 100 {
		p.SeverityThreshold = 100
	}
}

// RequiresAction fires on severity at or above the threshold, OR on an intent
// that deserves a reply. A pure-severity trigger would miss "onde compro?",
// a sales lead sitting unanswered under a post.
func (p ActionPolicy) RequiresAction(severity int, intent Intent) bool {
	p.Normalize()
	if severity >= p.SeverityThreshold {
		return true
	}
	switch intent {
	case IntentComplaint, IntentSupportRequest, IntentQuestion:
		return true
	}
	return false
}
