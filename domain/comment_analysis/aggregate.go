package comment_analysis

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/shared"
)

// Aggregates (§11). Live stats, rollups and author projections all carry the
// SAME Counters and derive their score with the SAME function, so a trend
// line, the number above it and an author's row cannot disagree.

// Counters is the §11.1 set: one COUNT(*) FILTER per field, exactly like the
// conversation AnalysisStats.
type Counters struct {
	Total    int `json:"total"`
	Analyzed int `json:"analyzed"`
	Pending  int `json:"pending"`
	InFlight int `json:"inFlight"`
	Failed   int `json:"failed"`
	Skipped  int `json:"skipped"`

	SentimentPositive int `json:"sentimentPositive"`
	SentimentNeutral  int `json:"sentimentNeutral"`
	SentimentNegative int `json:"sentimentNegative"`

	StanceSupporter int `json:"stanceSupporter"`
	StanceNeutral   int `json:"stanceNeutral"`
	StanceCritic    int `json:"stanceCritic"`
	StanceHostile   int `json:"stanceHostile"`

	IntentPraise         int `json:"intentPraise"`
	IntentQuestion       int `json:"intentQuestion"`
	IntentComplaint      int `json:"intentComplaint"`
	IntentSupportRequest int `json:"intentSupportRequest"`
	IntentSpam           int `json:"intentSpam"`
	IntentSalesLead      int `json:"intentSalesLead"`
	IntentOther          int `json:"intentOther"`

	SpamCount int `json:"spamCount"`

	SeverityAvg       float64 `json:"severityAvg"`
	SeverityMax       int     `json:"severityMax"`
	SeverityHighCount int     `json:"severityHighCount"` // ≥ HighSeverityThreshold

	RequiresActionCount int `json:"requiresActionCount"`

	DistinctAuthors int `json:"distinctAuthors"`
	FlaggedAuthors  int `json:"flaggedAuthors"`

	// ---- Conversation subjects ----
	//
	// Everything the legacy conversation-analysis stats block reported, in the
	// same aggregate as the comment counters rather than a second endpoint with
	// its own shape. A filtered slice can now contain both kinds, so the two
	// subject counts say how much of the slice each block describes: without
	// them a reader cannot tell an all-comment slice from one where every
	// conversation happened to be unlabelled.
	CommentCount      int `json:"commentCount"`
	ConversationCount int `json:"conversationCount"`

	InterestInterested    int `json:"interestInterested"`
	InterestNotInterested int `json:"interestNotInterested"`
	InterestUndecided     int `json:"interestUndecided"`

	DispositionSale        int `json:"dispositionSale"`
	DispositionFillingInfo int `json:"dispositionFillingInfo"`
	DispositionCallback    int `json:"dispositionCallback"`
	DispositionDeclined    int `json:"dispositionDeclined"`
	DispositionNoAnswer    int `json:"dispositionNoAnswer"`
	DispositionVoicemail   int `json:"dispositionVoicemail"`
	DispositionPending     int `json:"dispositionPending"`

	QualificationHotLead  int `json:"qualificationHotLead"`
	QualificationWarmLead int `json:"qualificationWarmLead"`
	QualificationColdLead int `json:"qualificationColdLead"`

	NextActionScheduleCallback int `json:"nextActionScheduleCallback"`
	NextActionSendWhatsApp     int `json:"nextActionSendWhatsApp"`
	NextActionClose            int `json:"nextActionClose"`
	NextActionEscalate         int `json:"nextActionEscalate"`
	NextActionContinue         int `json:"nextActionContinue"`

	// AttendanceQuality is averaged over ANALYSED CONVERSATIONS only. Comments
	// carry no such score, and including their zeros would drag the average of
	// a mixed slice toward nothing.
	AttendanceQualityAvg float64 `json:"attendanceQualityAvg"`
	AttendanceQualityMin int     `json:"attendanceQualityMin"`
	AttendanceQualityMax int     `json:"attendanceQualityMax"`

	MessagesTotal int     `json:"messagesTotal"`
	MessagesAvg   float64 `json:"messagesAvg"`
}

func (c Counters) StanceMix() StanceMix {
	return StanceMix{Supporter: c.StanceSupporter, Neutral: c.StanceNeutral, Critic: c.StanceCritic, Hostile: c.StanceHostile}
}

func (c Counters) score() int {
	return AcceptanceScore(c.StanceMix(), c.SeverityHighCount)
}

// TopicStat is one row of the topic breakdown.
type TopicStat struct {
	TopicKey          string  `json:"topicKey"`
	Count             int     `json:"count"`
	SentimentPositive int     `json:"sentimentPositive"`
	SentimentNeutral  int     `json:"sentimentNeutral"`
	SentimentNegative int     `json:"sentimentNegative"`
	SeverityAvg       float64 `json:"severityAvg"`
}

// Stats is the live aggregate for a filtered slice.
type Stats struct {
	Counters
	Topics          []TopicStat `json:"topics"`
	AcceptanceScore int         `json:"acceptanceScore"`
	// Finalized guards against serving counters whose score was never
	// derived: a repository fills Counters, the use case calls Finalize.
	Finalized bool `json:"-"`
}

func (s *Stats) Finalize() {
	s.AcceptanceScore = s.Counters.score()
	s.Finalized = true
}

// ---- Rollups ----

// RollupScope is what a rollup row summarises.
type RollupScope string

const (
	ScopeAccount   RollupScope = "account"
	ScopeContainer RollupScope = "container"
	ScopeTopic     RollupScope = "topic"
)

func (s RollupScope) Valid() bool {
	switch s {
	case ScopeAccount, ScopeContainer, ScopeTopic:
		return true
	}
	return false
}

// Rollup is one (scope, scopeID, day) snapshot: the row a 90-day trend
// reads instead of COUNT(*) FILTER over millions. Historical rollups never
// change when a comment is deleted today (§6.4).
type Rollup struct {
	WorkspaceID string      `json:"workspaceId"`
	Source      Source      `json:"source"`
	AccountID   string      `json:"accountId"`
	Scope       RollupScope `json:"scope"`
	ScopeID     string      `json:"scopeId"`
	BucketDate  time.Time   `json:"bucketDate"`
	Counters
	AcceptanceScore int       `json:"acceptanceScore"`
	ComputedAt      time.Time `json:"computedAt"`
}

func (r *Rollup) Finalize() {
	r.AcceptanceScore = r.Counters.score()
}

// BucketDate truncates an instant to its UTC calendar day.
func BucketDate(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// ---- Authors ----

// ModerationState is operator-set standing for an author.
type ModerationState string

const (
	ModerationNone    ModerationState = "none"
	ModerationWatched ModerationState = "watched"
	ModerationMuted   ModerationState = "muted"
	ModerationBlocked ModerationState = "blocked"
)

func (m ModerationState) Valid() bool {
	switch m {
	case ModerationNone, ModerationWatched, ModerationMuted, ModerationBlocked:
		return true
	}
	return false
}

// TopicCount is one entry of an author's top topics.
type TopicCount struct {
	TopicKey string `json:"topicKey"`
	Count    int    `json:"count"`
}

// AuthorStats is the projection behind "who commented bad things": one row
// per (source, account, author), rebuilt from their comments by the rollup
// job. The standing is derived from the HISTORY (§11.3).
type AuthorStats struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Source      Source `json:"source"`
	AccountID   string `json:"accountId"`

	AuthorExternalID string `json:"authorExternalId"`
	AuthorHandle     string `json:"authorHandle,omitempty"`

	FirstSeenAt time.Time `json:"firstSeenAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`

	Counters
	TopTopics []TopicCount `json:"topTopics"`

	DerivedStance   Stance          `json:"derivedStance"`
	IsFlagged       bool            `json:"isFlagged"`
	ModerationState ModerationState `json:"moderationState"`
	// Role is the §5 inference. Unlike everything above it, this is NOT a pure
	// function of the counters: it is model output over the person's own words,
	// so the rollup must preserve it the way it preserves ModerationState
	// rather than recomputing it.
	Role AuthorRoleInference `json:"role"`

	// Reputation is the signed ledger (§8): what this person's history adds up
	// to, unbounded, negative for a hostile one. Distinct from the account's
	// AcceptanceScore, which is 0..100 — see AuthorReputation for why both.
	Reputation int `json:"reputation"`

	UpdatedAt time.Time `json:"updatedAt"`
}

// Derive computes the standing from the counters.
//
// Everything here is a pure function of counts, never a stored opinion: the
// rollup can be rebuilt from the comments at any time and must land on the same
// numbers. That is also why Reputation is derived rather than accumulated —
// an incremented column drifts the moment one rollup is missed.
func (a *AuthorStats) Derive() {
	mix := a.StanceMix()
	a.DerivedStance = DerivedStance(mix)
	a.IsFlagged = IsFlagged(a.DerivedStance, a.SeverityHighCount)
	a.Reputation = AuthorReputation(mix, a.SeverityHighCount)
	if a.ModerationState == "" {
		a.ModerationState = ModerationNone
	}
	// Normalised, never recomputed: Derive owns the counters, not the claim
	// about who this person is.
	a.Role.Normalize()
}

// ---- Filters ----

// MaxTrendRange bounds a trend query; rollups are daily, so this is ~366
// rows per scope.
const MaxTrendRange = 366 * 24 * time.Hour

// ListInput filters the comment feed and the live stats. WorkspaceID is set
// by the use case from the caller's session, never from a query parameter.
type ListInput struct {
	WorkspaceID string
	Source      Source
	AccountID   string
	ContainerID string

	// SubjectKinds narrows to comments, conversations, or both. Empty means
	// every kind, so this stays an honest general-purpose filter; it is the
	// DELIVERY layer that pins the comment surface to comments, which keeps
	// "what this screen shows" a decision of the screen rather than a default
	// buried in the domain.
	SubjectKinds []SubjectKind

	// Conversation-only filters. Ignored for comment rows, which carry none of
	// these labels.
	Interest      Interest
	Disposition   Disposition
	Qualification Qualification
	NextAction    NextAction

	From *time.Time
	To   *time.Time

	Statuses         []Status
	TopicKey         string
	Stance           Stance
	Sentiment        shared.Sentiment
	Intent           Intent
	SeverityMin      *int
	SeverityMax      *int
	RequiresAction   *bool
	AuthorExternalID string

	Options shared.QueryOptions
}

func (in *ListInput) Normalize() {
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	in.AccountID = strings.TrimSpace(in.AccountID)
	in.ContainerID = strings.TrimSpace(in.ContainerID)
	in.AuthorExternalID = strings.TrimSpace(in.AuthorExternalID)
	in.TopicKey = NormalizeTopicKey(in.TopicKey)
	in.Options.Pagination = shared.NormalizePagination(in.Options.Pagination)
}

func (in ListInput) Validate() error {
	if in.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidFilter)
	}
	if in.Source != "" && !in.Source.Valid() {
		return fmt.Errorf("%w: source %q", ErrInvalidFilter, in.Source)
	}
	for _, s := range in.Statuses {
		if !s.Valid() {
			return fmt.Errorf("%w: status %q", ErrInvalidFilter, s)
		}
	}
	if in.Stance != "" && !in.Stance.Valid() {
		return fmt.Errorf("%w: stance %q", ErrInvalidFilter, in.Stance)
	}
	if in.Sentiment != "" && !in.Sentiment.Valid() {
		return fmt.Errorf("%w: sentiment %q", ErrInvalidFilter, in.Sentiment)
	}
	if in.Intent != "" && !in.Intent.Valid() {
		return fmt.Errorf("%w: intent %q", ErrInvalidFilter, in.Intent)
	}
	for _, k := range in.SubjectKinds {
		if !k.Valid() {
			return fmt.Errorf("%w: subject kind %q", ErrInvalidFilter, k)
		}
	}
	if in.Interest != "" && !in.Interest.Valid() {
		return fmt.Errorf("%w: interest %q", ErrInvalidFilter, in.Interest)
	}
	if in.Disposition != "" && !in.Disposition.Valid() {
		return fmt.Errorf("%w: disposition %q", ErrInvalidFilter, in.Disposition)
	}
	if in.Qualification != "" && !in.Qualification.Valid() {
		return fmt.Errorf("%w: qualification %q", ErrInvalidFilter, in.Qualification)
	}
	if in.NextAction != "" && !in.NextAction.Valid() {
		return fmt.Errorf("%w: next action %q", ErrInvalidFilter, in.NextAction)
	}
	for _, v := range []*int{in.SeverityMin, in.SeverityMax} {
		if v != nil && (*v < 0 || *v > 100) {
			return fmt.Errorf("%w: severity must be within 0..100", ErrInvalidFilter)
		}
	}
	if in.SeverityMin != nil && in.SeverityMax != nil && *in.SeverityMin > *in.SeverityMax {
		return fmt.Errorf("%w: severity range is inverted", ErrInvalidFilter)
	}
	if in.From != nil && in.To != nil && in.From.After(*in.To) {
		return fmt.Errorf("%w: date range is inverted", ErrInvalidFilter)
	}
	return nil
}

// AuthorsInput filters the ranked-authors table.
type AuthorsInput struct {
	WorkspaceID     string
	Source          Source
	AccountID       string
	FlaggedOnly     bool
	Stance          Stance
	ModerationState ModerationState
	MinComments     int

	// AuthorExternalID resolves one person to their row (§2). By external id
	// and not by handle: handles are renameable on every channel we mirror, so
	// a lookup by handle would find a different person after a rename, or
	// nobody at all.
	AuthorExternalID string

	// From and To narrow the ranking to a window, by the comment's own
	// timestamp.
	//
	// This changes where the answer COMES FROM, which is why it is worth a
	// comment. Without a range the ranking reads the author projection, which
	// holds lifetime counters. With one it cannot: the projection has no time
	// dimension, so the counters are regrouped from the comments themselves.
	// The two paths must agree on an all-time window, and an integration test
	// says so.
	From *time.Time
	To   *time.Time

	// Sort is the ranking (§1). The zero value is DefaultAuthorSort, applied by
	// Normalize, so every caller gets a deterministic order without asking —
	// an unordered page cannot be paged through without repeating rows.
	Sort Sort

	Options shared.QueryOptions
}

// MaxAuthorRankingRange bounds a windowed ranking. The lifetime ranking reads
// an indexed projection and is cheap at any size; a windowed one regroups the
// comments, so an unbounded range is a table scan somebody asked for by
// accident. A year covers every question anyone has actually asked.
const MaxAuthorRankingRange = 366 * 24 * time.Hour

// HasPeriod reports whether this is a windowed ranking. One end is enough:
// "desde o dia 1" is a real question and the lifetime table cannot answer it.
func (in AuthorsInput) HasPeriod() bool {
	return in.From != nil || in.To != nil
}

func (in *AuthorsInput) Normalize() {
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	in.AccountID = strings.TrimSpace(in.AccountID)
	in.AuthorExternalID = strings.TrimSpace(in.AuthorExternalID)
	if in.Sort.Key == "" {
		in.Sort = DefaultAuthorSort
	}
	in.Options.Pagination = shared.NormalizePagination(in.Options.Pagination)
}

func (in AuthorsInput) Validate() error {
	if in.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidFilter)
	}
	if in.Source != "" && !in.Source.Valid() {
		return fmt.Errorf("%w: source %q", ErrInvalidFilter, in.Source)
	}
	if in.Stance != "" && !in.Stance.Valid() {
		return fmt.Errorf("%w: stance %q", ErrInvalidFilter, in.Stance)
	}
	if in.ModerationState != "" && !in.ModerationState.Valid() {
		return fmt.Errorf("%w: moderation state %q", ErrInvalidFilter, in.ModerationState)
	}
	if in.From != nil && in.To != nil {
		if in.From.After(*in.To) {
			return fmt.Errorf("%w: date range is inverted", ErrInvalidFilter)
		}
		if in.To.Sub(*in.From) > MaxAuthorRankingRange {
			return fmt.Errorf("%w: date range exceeds %s", ErrInvalidFilter, MaxAuthorRankingRange)
		}
	}
	if in.MinComments < 0 {
		return fmt.Errorf("%w: min comments cannot be negative", ErrInvalidFilter)
	}
	if in.Sort.Key != "" && !in.Sort.Key.Valid() {
		return fmt.Errorf("%w: sort key %q", ErrInvalidFilter, in.Sort.Key)
	}
	return nil
}

// TrendInput selects a rollup series.
type TrendInput struct {
	WorkspaceID string
	Scope       RollupScope
	ScopeID     string
	From        time.Time
	To          time.Time
}

func (in TrendInput) Validate() error {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidFilter)
	}
	if !in.Scope.Valid() {
		return fmt.Errorf("%w: scope %q", ErrInvalidFilter, in.Scope)
	}
	if strings.TrimSpace(in.ScopeID) == "" {
		return fmt.Errorf("%w: scope id is required", ErrInvalidFilter)
	}
	if in.From.After(in.To) {
		return fmt.Errorf("%w: date range is inverted", ErrInvalidFilter)
	}
	if in.To.Sub(in.From) > MaxTrendRange {
		return fmt.Errorf("%w: date range exceeds %s", ErrInvalidFilter, MaxTrendRange)
	}
	return nil
}

// ---- Settings ----

// DefaultDailyCap is the per-workspace ceiling on analysed comments per day
// (§9.3). A post that goes viral overnight must not produce a bill nobody
// authorised.
const DefaultDailyCap = 20_000

// Settings is the per-account configuration of the engine. Channel-neutral
// and keyed by (source, account) so a second channel needs no new column on
// its own account entity.
type Settings struct {
	WorkspaceID string `json:"workspaceId"`
	Source      Source `json:"source"`
	AccountID   string `json:"accountId"`

	// Enabled is off by default: nothing runs, nothing is billed (§16).
	Enabled bool `json:"enabled"`
	// Model overrides the provider default; empty means the default.
	Model string `json:"model,omitempty"`
	// Vertical seeded the topics and is kept so "reset to defaults" works.
	Vertical Vertical `json:"vertical"`
	Topics   TopicSet `json:"topics"`

	ActionPolicy ActionPolicy `json:"actionPolicy"`
	// ReplyPolicy is what this account may say back (§6). Off by default, and
	// per account rather than per workspace: one brand voice may be safe to
	// automate and the next one next door may not.
	ReplyPolicy ReplyPolicy `json:"replyPolicy"`
	// DailyCap is the per-day analysed-comment ceiling for this workspace's
	// account. 0 means the default; the cap is never unlimited.
	DailyCap int `json:"dailyCap"`
	// Instructions is free text the operator writes about the account ("what
	// this account is about, what to watch for") that reaches the model as
	// context beside the post's caption. Optional.
	Instructions string `json:"instructions,omitempty"`

	UpdatedAt time.Time `json:"updatedAt"`
}

// NewSettings is what an account gets before an operator touches anything.
func NewSettings(workspaceID string, source Source, accountID string, vertical Vertical) Settings {
	s := Settings{WorkspaceID: workspaceID, Source: source, AccountID: accountID, Vertical: vertical}
	s.Normalize()
	return s
}

func (s *Settings) Normalize() {
	s.WorkspaceID = strings.TrimSpace(s.WorkspaceID)
	s.AccountID = strings.TrimSpace(s.AccountID)
	s.Model = strings.TrimSpace(s.Model)
	s.Instructions = strings.TrimSpace(s.Instructions)
	if !s.Vertical.Valid() {
		s.Vertical = VerticalServices
	}
	if len(s.Topics) == 0 {
		s.Topics = DefaultTopicsFor(s.Vertical)
	} else {
		s.Topics = s.Topics.Normalize()
	}
	s.ActionPolicy.Normalize()
	s.ReplyPolicy.Normalize()
	if s.DailyCap <= 0 {
		s.DailyCap = DefaultDailyCap
	}
}

func (s Settings) Validate() error {
	if s.WorkspaceID == "" {
		return ErrWorkspaceRequired
	}
	if !s.Source.Valid() || s.AccountID == "" {
		return ErrContainerInvalid
	}
	if err := s.Topics.Validate(); err != nil {
		return err
	}
	if s.DailyCap < 0 {
		return fmt.Errorf("%w: daily cap cannot be negative", ErrInvalidFilter)
	}
	return nil
}

// ---- Container overrides ----

// MaxInstructionsRunes bounds the free-text context so it cannot crowd the
// comments out of the input budget.
const MaxInstructionsRunes = 2000

// ContainerOverride is a post's own analysis settings, layered over the
// account's the way a post-scoped comment rule pre-empts an account default.
// Every field is optional: nil means "inherit". An override with no fields
// is legal and changes nothing.
type ContainerOverride struct {
	WorkspaceID string `json:"workspaceId"`
	Source      Source `json:"source"`
	AccountID   string `json:"accountId"`
	ContainerID string `json:"containerId"`

	Enabled           *bool     `json:"enabled,omitempty"`
	Model             *string   `json:"model,omitempty"`
	Topics            *TopicSet `json:"topics,omitempty"`
	SeverityThreshold *int      `json:"severityThreshold,omitempty"`
	// Instructions are ADDED to the account's, not swapped for them: the
	// account says what it is, the post says what this one is about.
	Instructions *string `json:"instructions,omitempty"`

	UpdatedAt time.Time `json:"updatedAt"`
}

func (o *ContainerOverride) Normalize() {
	o.WorkspaceID = strings.TrimSpace(o.WorkspaceID)
	o.AccountID = strings.TrimSpace(o.AccountID)
	o.ContainerID = strings.TrimSpace(o.ContainerID)
	if o.Model != nil {
		m := strings.TrimSpace(*o.Model)
		o.Model = &m
	}
	if o.Topics != nil {
		t := o.Topics.Normalize()
		o.Topics = &t
	}
	if o.SeverityThreshold != nil {
		p := ActionPolicy{SeverityThreshold: *o.SeverityThreshold}
		p.Normalize()
		o.SeverityThreshold = &p.SeverityThreshold
	}
	if o.Instructions != nil {
		i, _ := TruncateRunes(strings.TrimSpace(*o.Instructions), MaxInstructionsRunes)
		o.Instructions = &i
	}
}

func (o ContainerOverride) Validate() error {
	if o.WorkspaceID == "" {
		return ErrWorkspaceRequired
	}
	if !o.Source.Valid() || o.AccountID == "" || o.ContainerID == "" {
		return ErrContainerInvalid
	}
	if o.Topics != nil {
		if err := o.Topics.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// IsEmpty reports whether the override changes nothing; the API deletes such
// a row rather than storing an inheritance of everything.
func (o ContainerOverride) IsEmpty() bool {
	return o.Enabled == nil && o.Model == nil && o.Topics == nil && o.SeverityThreshold == nil &&
		(o.Instructions == nil || *o.Instructions == "")
}

// Ref is the container the override belongs to.
func (o ContainerOverride) Ref() ContainerRef {
	return ContainerRef{Source: o.Source, AccountID: o.AccountID, ContainerID: o.ContainerID}
}

// WithOverride returns the effective settings for a post: the receiver with
// the override's non-nil fields applied. The receiver is not mutated.
func (s Settings) WithOverride(o *ContainerOverride) Settings {
	if o == nil {
		return s
	}
	out := s
	if o.Enabled != nil {
		out.Enabled = *o.Enabled
	}
	if o.Model != nil && strings.TrimSpace(*o.Model) != "" {
		out.Model = strings.TrimSpace(*o.Model)
	}
	if o.Topics != nil && len(*o.Topics) > 0 {
		out.Topics = o.Topics.Normalize()
	}
	if o.SeverityThreshold != nil {
		out.ActionPolicy = ActionPolicy{SeverityThreshold: *o.SeverityThreshold}
		out.ActionPolicy.Normalize()
	}
	if o.Instructions != nil {
		if extra := strings.TrimSpace(*o.Instructions); extra != "" {
			if out.Instructions != "" {
				out.Instructions = out.Instructions + "\n\n" + extra
			} else {
				out.Instructions = extra
			}
		}
	}
	return out
}
