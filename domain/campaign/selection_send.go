package campaign

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"vozko/domain/whatsapp/template"
)

const (
	MaxEntries          = 150000
	MaxSelectionParts   = 10
	SourceLeadSelection = "lead_selection"
)

type Channel string

const (
	ChannelOfficial   Channel = "official"
	ChannelUnofficial Channel = "unofficial"
)

func (c Channel) Valid() bool {
	return c == ChannelOfficial || c == ChannelUnofficial
}

var (
	ErrSelectionEmpty             = errors.New("campaign: the selection holds no lead")
	ErrSelectionOverCampaignCap   = fmt.Errorf("campaign: one campaign takes at most %d leads, narrow the selection or split it into several campaigns", MaxEntries)
	ErrSelectionTooLarge          = fmt.Errorf("campaign: a send from a selection takes at most %d campaigns of %d leads", MaxSelectionParts, MaxEntries)
	ErrHeaderVariableUnsupported  = errors.New("campaign: templates with a variable in the header text cannot be sent to a selection yet")
	ErrNamedParametersUnsupported = errors.New("campaign: templates with named variables cannot be sent to a selection, use {{1}}, {{2}}")
	ErrUnaffordable               = errors.New("campaign: the balance does not cover this send")
	ErrOverMonthlyCap             = errors.New("campaign: this send goes over what is left of the workspace's monthly send cap")
	ErrFirstNInvalid              = errors.New("campaign: the number of leads to send to must be between 1 and the eligible count")
	ErrNothingEligible            = errors.New("campaign: no lead of this send can receive it")
	ErrCreationScopeMissing       = errors.New("campaign: the person creating the send is unknown, nothing was created")
	ErrDepartmentRequired         = errors.New("campaign: this workspace has departments, choose the department of the send")
	ErrNotFromSelection           = errors.New("campaign: this campaign was not prepared from a lead selection")
	ErrAlreadyStarted             = errors.New("campaign: this send already started and can no longer be cancelled")
	ErrSendIncomplete             = errors.New("campaign: not every campaign of this send was found, prepare it again")
	ErrSelectionSendLocked        = errors.New("campaign: a send prepared from a lead selection cannot be changed, cancel it and prepare it again")
	ErrSendPreparing              = errors.New("campaign: this send is still being prepared, try again in a moment")
	ErrSelectionStartNeedsReview  = errors.New("campaign: a send prepared from a lead selection starts from its review in the leads page, which skips leads already sending elsewhere and checks the balance and the monthly cap")
)

func CheckSelectionSize(selected int, split bool) (int, error) {
	if selected <= 0 {
		return 0, ErrSelectionEmpty
	}
	parts := PartsNeeded(selected)
	if parts > 1 && !split {
		return 0, ErrSelectionOverCampaignCap
	}
	if parts > MaxSelectionParts {
		return 0, ErrSelectionTooLarge
	}
	return parts, nil
}

func SplitSelection(selected int, split bool) ([]int, error) {
	parts, err := CheckSelectionSize(selected, split)
	if err != nil {
		return nil, err
	}
	sizes := make([]int, parts)
	for i := range sizes {
		sizes[i] = selected / parts
		if i < selected%parts {
			sizes[i]++
		}
	}
	return sizes, nil
}

func PartKeys(base string, parts int) []string {
	keys := make([]string, 0, parts)
	for part := 1; part <= parts; part++ {
		keys = append(keys, fmt.Sprintf("%s:%d/%d", base, part, parts))
	}
	return keys
}

func BaseOfKey(key string) string {
	at := strings.LastIndex(key, ":")
	if at <= 0 {
		return ""
	}
	return key[:at]
}

func PartsOfKey(key string) (int, bool) {
	base := BaseOfKey(key)
	if base == "" {
		return 0, false
	}
	partText, totalText, found := strings.Cut(key[len(base)+1:], "/")
	if !found {
		return 0, false
	}
	part, err := strconv.Atoi(partText)
	if err != nil {
		return 0, false
	}
	total, err := strconv.Atoi(totalText)
	if err != nil || part < 1 || part > total || total > MaxSelectionParts {
		return 0, false
	}
	return total, true
}

type SendBudget struct {
	Eligible        int
	Priced          bool
	UnitPriceMicros int64
	BalanceMicros   int64
	CapRemaining    *int64
}

type BudgetRefusal struct {
	Reason error
	Fits   int
}

func (r *BudgetRefusal) Error() string {
	return fmt.Sprintf("%v (at most %d leads fit)", r.Reason, r.Fits)
}

func (r *BudgetRefusal) Unwrap() error {
	return r.Reason
}

func (b SendBudget) unpriced() bool {
	return b.Priced && b.UnitPriceMicros <= 0
}

func (b SendBudget) affordable() int64 {
	if !b.Priced {
		return int64(b.Eligible)
	}
	if b.unpriced() {
		return 0
	}
	if b.BalanceMicros <= 0 {
		return 0
	}
	return b.BalanceMicros / b.UnitPriceMicros
}

func (b SendBudget) capped() int64 {
	if b.CapRemaining == nil {
		return int64(b.Eligible)
	}
	if *b.CapRemaining < 0 {
		return 0
	}
	return *b.CapRemaining
}

func (b SendBudget) Fits() int {
	fits := int64(b.Eligible)
	fits = min(fits, b.affordable(), b.capped())
	if fits < 0 {
		return 0
	}
	return int(fits)
}

func (b SendBudget) limit() error {
	if b.unpriced() {
		return template.ErrPricingUnavailable
	}
	if b.affordable() <= b.capped() {
		return ErrUnaffordable
	}
	return ErrOverMonthlyCap
}

func (b SendBudget) Allow(firstN int) (int, error) {
	if b.Eligible <= 0 {
		return 0, ErrNothingEligible
	}
	if b.unpriced() {
		return 0, template.ErrPricingUnavailable
	}
	if firstN < 0 || firstN > b.Eligible {
		return 0, ErrFirstNInvalid
	}
	wanted := b.Eligible
	if firstN > 0 {
		wanted = firstN
	}
	fits := b.Fits()
	if wanted > fits {
		return 0, &BudgetRefusal{Reason: b.limit(), Fits: fits}
	}
	return wanted, nil
}

func EstimatedDays(eligible, dailyCap int) int {
	if eligible <= 0 {
		return 0
	}
	if dailyCap <= 0 {
		return 1
	}
	return (eligible + dailyCap - 1) / dailyCap
}

func RefuseSelectionTemplate(headerVariables int, namedParameters bool) error {
	if namedParameters {
		return ErrNamedParametersUnsupported
	}
	if headerVariables > 0 {
		return ErrHeaderVariableUnsupported
	}
	return nil
}

var (
	ErrIdempotencyKeyTaken    = errors.New("campaign: another campaign of this workspace already holds this idempotency key")
	ErrIdempotencyUnavailable = errors.New("campaign: idempotent creation is not available, nothing was created")
)

var ErrLeadTargetsUnavailable = errors.New("campaign: lead targets cannot be resolved here, nothing was created")

type PartTally struct {
	CampaignID   string
	Entries      int
	Missing      map[MissingVariable]int
	CooldownDays int
	Tally
}

func SkipFailureCodes() []int {
	reasons := SkipReasons()
	codes := make([]int, 0, len(reasons))
	for _, reason := range reasons {
		codes = append(codes, reason.FailureCode())
	}
	return codes
}

func (b SendBudget) Refusal() error {
	if b.Fits() >= b.Eligible {
		return nil
	}
	return b.limit()
}

type SendQuote struct {
	Count           int    `json:"count"`
	Parts           int    `json:"parts"`
	SplitRequired   bool   `json:"splitRequired"`
	MaxPerCampaign  int    `json:"maxPerCampaign"`
	Category        string `json:"category,omitempty"`
	UnitPriceMicros int64  `json:"unitPriceMicros"`
	CostMicros      int64  `json:"costMicros"`
	BalanceMicros   int64  `json:"balanceMicros"`
	Currency        string `json:"currency,omitempty"`
	Affordable      bool   `json:"affordable"`
	CapRemaining    *int64 `json:"capRemaining,omitempty"`
	Fits            int    `json:"fits"`
	Refusal         string `json:"refusal,omitempty"`
	DailyCap        int    `json:"dailyCap,omitempty"`
	EstimatedDays   int    `json:"estimatedDays,omitempty"`
}

type SendPart struct {
	CampaignID string `json:"campaignId"`
	Name       string `json:"name"`
	Status     Status `json:"status"`
	Entries    int    `json:"entries"`
	Eligible   int    `json:"eligible"`
}

type SendReview struct {
	Channel          Channel                `json:"channel"`
	Parts            []SendPart             `json:"parts"`
	Entries          int                    `json:"entries"`
	Eligible         int                    `json:"eligible"`
	Skipped          map[SkipReason]int     `json:"skipped"`
	Counted          map[CountedReason]int  `json:"counted"`
	MissingVariables []MissingVariableCount `json:"missingVariables"`
	CooldownDays     int                    `json:"cooldownDays,omitempty"`
	Quote            SendQuote              `json:"quote"`
	Started          bool                   `json:"started"`
}

func (r SendReview) Budget(eligible int) SendBudget {
	budget := SendBudget{Eligible: eligible, CapRemaining: r.Quote.CapRemaining}
	if r.Channel == ChannelOfficial {
		budget.Priced, budget.UnitPriceMicros, budget.BalanceMicros = true, r.Quote.UnitPriceMicros, r.Quote.BalanceMicros
	}
	return budget
}

func (r *SendReview) Clone() *SendReview {
	if r == nil {
		return nil
	}
	out := *r
	out.Parts = slices.Clone(r.Parts)
	out.Skipped = maps.Clone(r.Skipped)
	out.Counted = maps.Clone(r.Counted)
	out.MissingVariables = slices.Clone(r.MissingVariables)
	if r.Quote.CapRemaining != nil {
		remaining := *r.Quote.CapRemaining
		out.Quote.CapRemaining = &remaining
	}
	return &out
}

func PartsNeeded(count int) int {
	if count <= 0 {
		return 0
	}
	return (count + MaxEntries - 1) / MaxEntries
}

func (q *SendQuote) Size(selected int, split bool) error {
	q.Count, q.Parts, q.MaxPerCampaign = selected, PartsNeeded(selected), MaxEntries
	q.SplitRequired = q.Parts > 1 && !split
	if q.Parts > MaxSelectionParts {
		return ErrSelectionTooLarge
	}
	return nil
}

func (q *SendQuote) Judge(b SendBudget) {
	q.CapRemaining = b.CapRemaining
	q.Fits = b.Fits()
	q.Refusal = ErrorCode(b.Refusal())
}

func NewReview(channel Channel, parts []SendPart, tallies []PartTally) SendReview {
	review := SendReview{Channel: channel, Parts: parts, Skipped: map[SkipReason]int{}, Counted: map[CountedReason]int{}}
	byID := make(map[string]PartTally, len(tallies))
	for _, t := range tallies {
		byID[t.CampaignID] = t
	}
	missing := map[MissingVariable]int{}
	for i, part := range review.Parts {
		t := byID[part.CampaignID]
		review.Parts[i].Entries, review.Parts[i].Eligible = t.Entries, t.Eligible
		review.Entries += t.Entries
		review.Eligible += t.Eligible
		for reason, n := range t.Skipped {
			review.Skipped[reason] += n
		}
		for variable, n := range t.Missing {
			missing[variable] += n
		}
		review.CooldownDays = max(review.CooldownDays, t.CooldownDays)
		for reason, n := range t.Counted {
			review.Counted[reason] += n
		}
		if part.Status != StatusStopped || t.Entries-t.Eligible-t.SkippedTotal() > 0 {
			review.Started = true
		}
	}
	review.MissingVariables = missingVariableCounts(missing)
	return review
}

type KeyedPart struct {
	ID  string
	Key string
}

func CompleteParts(base string, found []KeyedPart) ([]string, bool) {
	if len(found) == 0 {
		return nil, false
	}
	parts, ok := PartsOfKey(found[0].Key)
	if !ok {
		return nil, false
	}
	byKey := make(map[string]string, len(found))
	for _, f := range found {
		byKey[f.Key] = f.ID
	}
	ids := make([]string, 0, parts)
	for _, key := range PartKeys(base, parts) {
		id, present := byKey[key]
		if !present {
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

type SendPartRef struct {
	CampaignID      string
	Key             string
	TemplateID      string
	BusinessPhoneID string
	InstanceID      string
}

func OneSend(parts []SendPartRef) error {
	if len(parts) == 0 {
		return ErrSendIncomplete
	}
	first := parts[0]
	base := BaseOfKey(first.Key)
	if base == "" {
		return ErrSendIncomplete
	}
	keyed := make([]KeyedPart, 0, len(parts))
	for _, p := range parts {
		if p.TemplateID != first.TemplateID || p.BusinessPhoneID != first.BusinessPhoneID || p.InstanceID != first.InstanceID {
			return ErrSendIncomplete
		}
		keyed = append(keyed, KeyedPart{ID: p.CampaignID, Key: p.Key})
	}
	ordered, complete := CompleteParts(base, keyed)
	if !complete || len(ordered) != len(parts) {
		return ErrSendIncomplete
	}
	return nil
}

func KeepFirst(eligible []int, allowed int) []int {
	kept := make([]int, len(eligible))
	left := max(allowed, 0)
	for i, n := range eligible {
		kept[i] = min(left, max(n, 0))
		left -= kept[i]
	}
	return kept
}

func SelectionStartGated(source string, action Action) bool {
	return source == SourceLeadSelection && action == ActionStart
}

func RefuseSelectionChange(source string, changesWhatIsSent bool) error {
	if source == SourceLeadSelection && changesWhatIsSent {
		return ErrSelectionSendLocked
	}
	return nil
}

type StartAttempt struct {
	Source   string
	Action   Action
	From     Status
	Reviewed bool
}

func (a StartAttempt) Gated() bool {
	return SelectionStartGated(a.Source, a.Action)
}

func (a StartAttempt) RefuseUnreviewed() error {
	if !a.Gated() || a.Reviewed || NormalizeStatus(a.From) == StatusPaused {
		return nil
	}
	return ErrSelectionStartNeedsReview
}
