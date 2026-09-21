package audience

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/shared"
)

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
	SeverityHighCount int     `json:"severityHighCount"`

	RequiresActionCount int `json:"requiresActionCount"`

	DistinctAuthors int `json:"distinctAuthors"`
	FlaggedAuthors  int `json:"flaggedAuthors"`

	CommentCount         int        `json:"commentCount"`
	ConversationCount    int        `json:"conversationCount"`
	ConversationAnalyzed int        `json:"conversationAnalyzed"`
	LastAnalyzedAt       *time.Time `json:"lastAnalyzedAt,omitempty"`

	InterestInterested    int `json:"interestInterested"`
	InterestNotInterested int `json:"interestNotInterested"`
	InterestUndecided     int `json:"interestUndecided"`

	DispositionSale        int `json:"dispositionSale"`
	DispositionFillingInfo int `json:"dispositionFillingInfo"`
	DispositionCallback    int `json:"dispositionCallback"`
	DispositionDeclined    int `json:"dispositionDeclined"`
	DispositionPending     int `json:"dispositionPending"`

	QualificationHotLead  int `json:"qualificationHotLead"`
	QualificationWarmLead int `json:"qualificationWarmLead"`
	QualificationColdLead int `json:"qualificationColdLead"`

	NextActionScheduleCallback int `json:"nextActionScheduleCallback"`
	NextActionSendWhatsApp     int `json:"nextActionSendWhatsApp"`
	NextActionClose            int `json:"nextActionClose"`
	NextActionEscalate         int `json:"nextActionEscalate"`
	NextActionContinue         int `json:"nextActionContinue"`

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

type TopicStat struct {
	TopicKey          string  `json:"topicKey"`
	Count             int     `json:"count"`
	SentimentPositive int     `json:"sentimentPositive"`
	SentimentNeutral  int     `json:"sentimentNeutral"`
	SentimentNegative int     `json:"sentimentNegative"`
	SeverityAvg       float64 `json:"severityAvg"`
}

type Stats struct {
	Counters
	Topics          []TopicStat    `json:"topics"`
	Subjects        []SubjectCount `json:"subjects"`
	AcceptanceScore int            `json:"acceptanceScore"`
	Finalized       bool           `json:"-"`
}

func (s *Stats) Finalize() {
	s.AcceptanceScore = s.Counters.score()
	s.Finalized = true
}

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

func BucketDate(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

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

type TopicCount struct {
	TopicKey string `json:"topicKey"`
	Count    int    `json:"count"`
}

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

	DerivedStance   Stance              `json:"derivedStance"`
	IsFlagged       bool                `json:"isFlagged"`
	ModerationState ModerationState     `json:"moderationState"`
	Role            AuthorRoleInference `json:"role"`

	Reputation int `json:"reputation"`

	UpdatedAt time.Time `json:"updatedAt"`
}

func (a *AuthorStats) Derive() {
	mix := a.StanceMix()
	a.DerivedStance = DerivedStance(mix)
	a.IsFlagged = IsFlagged(a.DerivedStance, a.SeverityHighCount)
	a.Reputation = AuthorReputation(mix, a.SeverityHighCount)
	if a.ModerationState == "" {
		a.ModerationState = ModerationNone
	}
	a.Role.Normalize()
}

const MaxTrendRange = 366 * 24 * time.Hour

type ListInput struct {
	WorkspaceID string
	Source      Source
	AccountID   string
	ContainerID string

	SubjectKinds []SubjectKind

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
	SubjectID        string
	LatestOnly       bool

	Options shared.QueryOptions
}

func (in *ListInput) Normalize() {
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	in.AccountID = strings.TrimSpace(in.AccountID)
	in.ContainerID = strings.TrimSpace(in.ContainerID)
	in.AuthorExternalID = strings.TrimSpace(in.AuthorExternalID)
	in.SubjectID = strings.TrimSpace(in.SubjectID)
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

type AuthorsInput struct {
	WorkspaceID     string
	Source          Source
	AccountID       string
	FlaggedOnly     bool
	Stance          Stance
	ModerationState ModerationState
	MinComments     int

	AuthorExternalID string

	From *time.Time
	To   *time.Time

	Sort Sort

	Options shared.QueryOptions
}

const MaxAuthorRankingRange = 366 * 24 * time.Hour

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

const DefaultDailyCap = 20_000

type Settings struct {
	WorkspaceID string `json:"workspaceId"`
	Source      Source `json:"source"`
	AccountID   string `json:"accountId"`

	Enabled  bool     `json:"enabled"`
	Model    string   `json:"model,omitempty"`
	Vertical Vertical `json:"vertical"`
	Topics   TopicSet `json:"topics"`

	ActionPolicy ActionPolicy `json:"actionPolicy"`
	ReplyPolicy  ReplyPolicy  `json:"replyPolicy"`
	DailyCap     int          `json:"dailyCap"`
	Instructions string       `json:"instructions,omitempty"`

	UpdatedAt time.Time `json:"updatedAt"`
}

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

const MaxInstructionsRunes = 2000

type ContainerOverride struct {
	WorkspaceID string `json:"workspaceId"`
	Source      Source `json:"source"`
	AccountID   string `json:"accountId"`
	ContainerID string `json:"containerId"`

	Enabled           *bool     `json:"enabled,omitempty"`
	Model             *string   `json:"model,omitempty"`
	Topics            *TopicSet `json:"topics,omitempty"`
	SeverityThreshold *int      `json:"severityThreshold,omitempty"`
	Instructions      *string   `json:"instructions,omitempty"`

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

func (o ContainerOverride) IsEmpty() bool {
	return o.Enabled == nil && o.Model == nil && o.Topics == nil && o.SeverityThreshold == nil &&
		(o.Instructions == nil || *o.Instructions == "")
}

func (o ContainerOverride) Ref() ContainerRef {
	return ContainerRef{Source: o.Source, AccountID: o.AccountID, ContainerID: o.ContainerID}
}

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
