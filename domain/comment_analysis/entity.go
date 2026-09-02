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
package comment_analysis

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

// Source names the channel a comment came from. The engine is channel-neutral:
// a new channel registers a source and gets the whole feature.
type Source string

const SourceInstagram Source = "instagram"

func (s Source) Valid() bool {
	return s == SourceInstagram
}

// ContainerRef identifies the thing comments sit under (for Instagram, a
// post). Debounce, locking and the backstop sweep are all keyed on it, because
// a post taking 5,000 comments in ten minutes must produce ONE unit of work,
// not 5,000.
type ContainerRef struct {
	Source      Source
	AccountID   string
	ContainerID string
}

// Key renders the ref as the Redis hash field and the lock name. Pinned
// format; ParseContainerKey is its inverse.
func (r ContainerRef) Key() string {
	return string(r.Source) + ":" + r.AccountID + ":" + r.ContainerID
}

func (r ContainerRef) Validate() error {
	if !r.Source.Valid() || r.AccountID == "" || r.ContainerID == "" {
		return ErrContainerInvalid
	}
	return nil
}

// ParseContainerKey is the inverse of Key.
func ParseContainerKey(key string) (ContainerRef, error) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 {
		return ContainerRef{}, fmt.Errorf("%w: %q", ErrContainerInvalid, key)
	}
	r := ContainerRef{Source: Source(parts[0]), AccountID: parts[1], ContainerID: parts[2]}
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

// CommentAnalysis is one classified comment.
type CommentAnalysis struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`

	Source          Source  `json:"source"`
	AccountID       string  `json:"accountId"`
	ContainerID     string  `json:"containerId"`
	SourceCommentID string  `json:"sourceCommentId"`
	ParentCommentID *string `json:"parentCommentId,omitempty"`

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

	// RequiresAction is DERIVED from severity and intent; see ActionPolicy.
	RequiresAction bool   `json:"requiresAction"`
	Excerpt        string `json:"excerpt"`
	// Truncated records that the text was cut before it reached the model or
	// the excerpt, so a classification of a partial comment is never silent.
	Truncated bool `json:"truncated"`

	BatchID    string     `json:"batchId,omitempty"`
	Model      string     `json:"model,omitempty"`
	AnalyzedAt *time.Time `json:"analyzedAt,omitempty"`
	// CommentedAt is when the comment was POSTED on the channel; CreatedAt
	// is when it reached us. Rollups bucket by the former, so a backfilled
	// comment lands on its own day.
	CommentedAt time.Time `json:"commentedAt"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// DeletedAt is set when the source comment was deleted (§6.4). The row
	// leaves the feed and the live stats but stays in historical rollups: the
	// rollup for last Tuesday must not change because someone deleted a
	// comment today.
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}

// SoftDelete tombstones the row. Idempotent.
func (a *CommentAnalysis) SoftDelete(now time.Time) {
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
	SourceCommentID  string
	ParentCommentID  string
	AuthorExternalID string
	AuthorHandle     string
	Text             string
	// CommentedAt is the channel's timestamp for the comment; zero falls
	// back to Now.
	CommentedAt time.Time
	Now         time.Time
}

// NewPending builds the row ingest inserts. A blank comment is recorded as
// skipped rather than pending: the totals stay honest and the model is never
// sent nothing to classify.
func NewPending(in NewInput) (*CommentAnalysis, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, ErrWorkspaceRequired
	}
	if strings.TrimSpace(in.SourceCommentID) == "" {
		return nil, ErrSourceCommentIDRequired
	}
	if err := in.Container.Validate(); err != nil {
		return nil, err
	}

	excerpt, cut := Excerpt(in.Text)
	a := &CommentAnalysis{
		WorkspaceID:      in.WorkspaceID,
		Source:           in.Container.Source,
		AccountID:        in.Container.AccountID,
		ContainerID:      in.Container.ContainerID,
		SourceCommentID:  strings.TrimSpace(in.SourceCommentID),
		AuthorExternalID: strings.TrimSpace(in.AuthorExternalID),
		AuthorHandle:     strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(in.AuthorHandle), "@")),
		Status:           StatusPending,
		Excerpt:          excerpt,
		Truncated:        cut,
		CommentedAt:      in.CommentedAt,
		CreatedAt:        in.Now,
		UpdatedAt:        in.Now,
	}
	if a.CommentedAt.IsZero() {
		a.CommentedAt = in.Now
	}
	if p := strings.TrimSpace(in.ParentCommentID); p != "" {
		a.ParentCommentID = &p
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
func (a *CommentAnalysis) Container() ContainerRef {
	return ContainerRef{Source: a.Source, AccountID: a.AccountID, ContainerID: a.ContainerID}
}

func (a *CommentAnalysis) transition(next Status, now time.Time) error {
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
func (a *CommentAnalysis) Claim(now time.Time) error {
	if err := a.transition(StatusInFlight, now); err != nil {
		return err
	}
	a.Attempts++
	return nil
}

// Release is the reconcile path for a claimed row the model did not answer
// for (or a stale in_flight row the backstop found). It goes back to pending
// to be retried, until MaxAttempts turns it into a visible failure.
func (a *CommentAnalysis) Release(reason string, now time.Time) {
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
func (a *CommentAnalysis) Unclaim(now time.Time) {
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
func (a *CommentAnalysis) MarkSkipped(reason string, now time.Time) error {
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
func (a *CommentAnalysis) Fail(reason string, now time.Time) error {
	if err := a.transition(StatusFailed, now); err != nil {
		return err
	}
	a.FailureReason = reason
	return nil
}

// Retry re-queues a failed row with a fresh set of attempts. Only an operator
// calls this, so giving the row its full allowance again is the intent.
func (a *CommentAnalysis) Retry(now time.Time) error {
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
func (a *CommentAnalysis) Apply(c Classification, policy ActionPolicy, prov Provenance, now time.Time) error {
	if err := a.transition(StatusAnalyzed, now); err != nil {
		return err
	}
	policy.Normalize()

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
