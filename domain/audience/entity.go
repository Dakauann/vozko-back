package audience

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/shared"
)

const (
	ExcerptMaxRunes = 200

	MaxAttempts = 3

	MinMessagesBetweenAnalyses = 2

	DefaultActionThreshold = 60

	MaxSummaryRunes = 1200

	MaxProductInterestRunes = 120

	MaxTranscriptRunes = 24_000
)

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

type Source string

const (
	SourceInstagram          Source = Source(shared.EntryTypeInstagram)
	SourceWhatsApp           Source = Source(shared.EntryTypeWhatsApp)
	SourceTelegram           Source = Source(shared.EntryTypeTelegram)
	SourceUnofficialWhatsApp Source = Source(shared.EntryTypeUnofficialWhatsApp)
)

func (s Source) EntryType() shared.EntryType { return shared.EntryType(s) }

func SourceOf(e shared.EntryType) Source { return Source(e) }

func (s Source) Valid() bool {
	return s.EntryType().SupportsAnalysis()
}

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

func (k SubjectKind) SupportedOn(s Source) bool {
	switch k {
	case SubjectKindComment:
		return s.EntryType().SupportsCommentAnalysis()
	case SubjectKindConversation:
		return s.EntryType().SupportsConversationAnalysis()
	}
	return false
}

type ContainerRef struct {
	Kind        SubjectKind
	Source      Source
	AccountID   string
	ContainerID string
}

func (r ContainerRef) withDefaults() ContainerRef {
	if r.Kind == "" {
		r.Kind = SubjectKindComment
	}
	return r
}

func (r ContainerRef) Key() string {
	r = r.withDefaults()
	base := string(r.Source) + ":" + r.AccountID + ":" + r.ContainerID
	if r.Kind == SubjectKindComment {
		return base
	}
	return string(r.Kind) + ":" + base
}

func (r ContainerRef) Normalized() ContainerRef { return r.withDefaults() }

func (r ContainerRef) Equal(o ContainerRef) bool {
	return r.withDefaults() == o.withDefaults()
}

func (r ContainerRef) Validate() error {
	r = r.withDefaults()
	if !r.Kind.Valid() || r.AccountID == "" || r.ContainerID == "" {
		return ErrContainerInvalid
	}
	if !r.Kind.SupportedOn(r.Source) {
		return ErrContainerInvalid
	}
	return nil
}

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

func (s Status) IsTerminal() bool {
	return s == StatusAnalyzed || s == StatusSkipped
}

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

type Analysis struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`

	SubjectKind SubjectKind `json:"subjectKind,omitempty"`
	Revision    string      `json:"revision,omitempty"`
	Transcript  string      `json:"-"`

	Source          Source  `json:"source"`
	AccountID       string  `json:"accountId"`
	ContainerID     string  `json:"containerId"`
	SubjectID       string  `json:"subjectId"`
	ParentSubjectID *string `json:"parentCommentId,omitempty"`

	AuthorExternalID string `json:"authorExternalId"`
	AuthorHandle     string `json:"authorHandle,omitempty"`

	Status        Status `json:"status"`
	Attempts      int    `json:"attempts"`
	FailureReason string `json:"failureReason,omitempty"`

	Sentiment shared.Sentiment `json:"sentiment,omitempty"`
	Stance    Stance           `json:"stance,omitempty"`
	Intent    Intent           `json:"intent,omitempty"`
	TopicKey  string           `json:"topicKey,omitempty"`
	IsSpam    bool             `json:"isSpam"`
	Language  string           `json:"language,omitempty"`

	Toxicity       shared.QualityLevel `json:"toxicity,omitempty"`
	PersonalAttack shared.QualityLevel `json:"personalAttack,omitempty"`
	LegalRisk      shared.QualityLevel `json:"legalRisk,omitempty"`
	Severity       int                 `json:"severity"`

	Interest           Interest      `json:"interest,omitempty"`
	ProductInterest    string        `json:"productInterest,omitempty"`
	ProductInterestKey string        `json:"productInterestKey,omitempty"`
	Disposition        Disposition   `json:"disposition,omitempty"`
	Qualification      Qualification `json:"qualification,omitempty"`
	NextAction         NextAction    `json:"nextAction,omitempty"`
	Summary            string        `json:"summary,omitempty"`
	AttendanceQuality  int           `json:"attendanceQuality,omitempty"`
	MessageCount       int           `json:"messageCount,omitempty"`

	RequiresAction bool   `json:"requiresAction"`
	Excerpt        string `json:"excerpt"`
	Truncated      bool   `json:"truncated"`

	BatchID    string     `json:"batchId,omitempty"`
	Model      string     `json:"model,omitempty"`
	AnalyzedAt *time.Time `json:"analyzedAt,omitempty"`
	OccurredAt time.Time  `json:"occurredAt"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	DeletedAt  *time.Time `json:"deletedAt,omitempty"`
}

func (a *Analysis) SoftDelete(now time.Time) {
	if a.DeletedAt != nil {
		return
	}
	a.DeletedAt = &now
	a.UpdatedAt = now
}

type NewInput struct {
	WorkspaceID      string
	Container        ContainerRef
	SubjectID        string
	ParentSubjectID  string
	AuthorExternalID string
	AuthorHandle     string
	Text             string
	OccurredAt       time.Time
	Now              time.Time
}

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
		SubjectID:        strings.TrimSpace(in.SubjectID),
		AuthorExternalID: strings.TrimSpace(in.AuthorExternalID),
		AuthorHandle:     strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(in.AuthorHandle), "@")),
		Status:           StatusPending,
		Excerpt:          excerpt,
		Truncated:        cut,
		OccurredAt:       in.OccurredAt,
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

func Excerpt(text string) (string, bool) {
	return TruncateRunes(strings.TrimSpace(text), ExcerptMaxRunes)
}

func TruncateRunes(s string, max int) (string, bool) {
	return shared.TruncateRunes(s, max)
}

func (a *Analysis) Container() ContainerRef {
	return ContainerRef{
		Kind: a.SubjectKind, Source: a.Source,
		AccountID: a.AccountID, ContainerID: a.ContainerID,
	}.withDefaults()
}

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

func (a *Analysis) Claim(now time.Time) error {
	if err := a.transition(StatusInFlight, now); err != nil {
		return err
	}
	a.Attempts++
	return nil
}

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

func (a *Analysis) Unclaim(now time.Time) {
	if a.Status != StatusInFlight {
		return
	}
	_ = a.transition(StatusPending, now)
	if a.Attempts > 0 {
		a.Attempts--
	}
}

func (a *Analysis) MarkSkipped(reason string, now time.Time) error {
	if a.Status == StatusInFlight {
		a.Unclaim(now)
	}
	if err := a.transition(StatusSkipped, now); err != nil {
		return err
	}
	a.FailureReason = reason
	return nil
}

func (a *Analysis) MissedRead(reason string, now time.Time) bool {
	a.Attempts++
	if a.Attempts >= MaxAttempts {
		_ = a.MarkSkipped(reason, now)
		return true
	}
	a.FailureReason = reason
	a.UpdatedAt = now
	return false
}

func (a *Analysis) WorthReanalysing(previous *Analysis) bool {
	if previous == nil {
		return true
	}
	if previous.Status == StatusPending || previous.Status == StatusInFlight {
		return false
	}
	if previous.Status != StatusAnalyzed {
		return true
	}
	return a.MessageCount-previous.MessageCount >= MinMessagesBetweenAnalyses
}

func (a *Analysis) HasSnapshot() bool {
	return a.Kind() == SubjectKindConversation && a.Revision != "" && a.Transcript != ""
}

func (a *Analysis) Fail(reason string, now time.Time) error {
	if err := a.transition(StatusFailed, now); err != nil {
		return err
	}
	a.FailureReason = reason
	return nil
}

func (a *Analysis) Retry(now time.Time) error {
	if err := a.transition(StatusPending, now); err != nil {
		return err
	}
	a.Attempts = 0
	a.FailureReason = ""
	return nil
}

type Provenance struct {
	BatchID string
	Model   string
}

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

func (a *Analysis) applyConversation(c Classification, prov Provenance, now time.Time) {
	a.Sentiment = c.Sentiment
	a.Interest = c.Interest
	subject := strings.TrimSpace(c.ProductInterest)
	a.ProductInterestKey = SubjectKey(subject)
	if a.ProductInterestKey == "" {
		subject = ""
	}
	a.ProductInterest = subject
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

	Interest        Interest
	ProductInterest string
	Disposition     Disposition
	Qualification   Qualification
	NextAction      NextAction
	Summary         string
	Quality         ConversationQuality
}

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

func (c *Classification) ValidateFor(kind SubjectKind, topics TopicSet) error {
	switch kind {
	case SubjectKindConversation:
		return c.ValidateConversation()
	case SubjectKindComment, "":
		return c.Validate(topics)
	}
	return fmt.Errorf("%w: subject kind %q", ErrInvalidClassification, kind)
}

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

func (c Classification) Severity() int {
	return shared.WeightedScore(SeverityDimensions(), c.levelFor)
}

type ActionPolicy struct {
	SeverityThreshold int
}

func (p *ActionPolicy) Normalize() {
	if p.SeverityThreshold <= 0 {
		p.SeverityThreshold = DefaultActionThreshold
	}
	if p.SeverityThreshold > 100 {
		p.SeverityThreshold = 100
	}
}

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
