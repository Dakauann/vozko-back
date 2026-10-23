package campaignaudience

import (
	"strings"

	"vozko/domain/campaign"
)

type Outcome string

const (
	OutcomeSend      Outcome = "send"
	OutcomeOut       Outcome = "out"
	OutcomeUndecided Outcome = "undecided"
)

type Blocker string

const (
	BlockerChoiceRequired           Blocker = "variable_choice_required"
	BlockerSourceRequired           Blocker = "variable_source_required"
	BlockerListVariablesMissing     Blocker = "list_variables_missing"
	BlockerListMetadataMissing      Blocker = "list_metadata_missing"
	BlockerLeadsMetadataUnavailable Blocker = "leads_metadata_unavailable"
	BlockerTemplateRequired         Blocker = "template_required"
	BlockerMessageRequired          Blocker = "message_required"
)

const ReasonMissingVariable = string(campaign.SkipMissingVariable)

type Candidate struct {
	Line         int
	LeadID       string
	Contact      string
	Name         string
	HasContact   bool
	Refusal      string
	Values       []string
	Metadata     map[string]any
	FirstContact bool
}

type Decision struct {
	Outcome         Outcome
	Reason          string
	Values          []string
	Missing         []campaign.MissingVariable
	Empty           []string
	MetadataMissing []string
}

type VariableCount struct {
	Key            string                 `json:"key"`
	Name           string                 `json:"name,omitempty"`
	Header         bool                   `json:"header"`
	Source         campaign.BindingSource `json:"source,omitempty"`
	Value          string                 `json:"value,omitempty"`
	Missing        int                    `json:"missing"`
	Policy         MissingPolicy          `json:"policy"`
	Text           string                 `json:"text,omitempty"`
	Fallback       string                 `json:"fallback,omitempty"`
	Suggested      bool                   `json:"suggested"`
	ChoiceRequired bool                   `json:"choiceRequired"`
}

type SelectionCount struct {
	Selected     int    `json:"selected"`
	ConfirmCount int    `json:"confirmCount"`
	Fingerprint  string `json:"fingerprint,omitempty"`
}

type ListCount struct {
	ID       string `json:"id"`
	Rows     int    `json:"rows"`
	Existing int    `json:"existing"`
	New      int    `json:"new"`
}

type Count struct {
	Channel           Channel         `json:"channel"`
	Source            Source          `json:"source"`
	Matched           int             `json:"matched"`
	WithContact       int             `json:"withContact"`
	Sendable          *int            `json:"sendable"`
	Out               map[string]int  `json:"out"`
	OutTotal          int             `json:"outTotal"`
	RequiredVariables int             `json:"requiredVariables"`
	Variables         []VariableCount `json:"variables"`
	Blockers          []Blocker       `json:"blockers"`
	Selection         *SelectionCount `json:"selection"`
	List              *ListCount      `json:"list"`
	MetadataMissing   []MetadataGap   `json:"metadataMissing"`
}

type Tally struct {
	channel   Channel
	source    Source
	bound     []Bound
	missing   []int
	matched   int
	contact   int
	sendable  int
	undecided int
	out       map[string]int
	metadata  *MetadataGaps
	awaits    Blocker
}

func NewTally(channel Channel, source Source, bound []Bound) *Tally {
	return &Tally{channel: channel, source: source, bound: bound, missing: make([]int, len(bound)), out: map[string]int{}, metadata: NewMetadataGaps(nil)}
}

func (t *Tally) Requiring(rule MetadataRule) *Tally {
	t.metadata = NewMetadataGaps(rule)
	return t
}

func (t *Tally) Awaiting(blocker Blocker) *Tally {
	t.awaits = blocker
	return t
}

func (t *Tally) sourcesRequired() bool {
	return t.source == SourceLeads && t.channel.bindsSources()
}

func (t *Tally) skipsSlot(i int) bool {
	b := t.bound[i]
	return Unbound(b) && (t.sourcesRequired() || b.Source == campaign.BindLiteral)
}

func (t *Tally) Add(c Candidate) Decision {
	t.matched++
	if c.HasContact {
		t.contact++
	}
	var d Decision
	if t.source == SourceList {
		d = t.addRow(c)
	} else {
		d = t.addLead(c)
	}
	d.MetadataMissing = t.metadata.Add(c.Line, c.Metadata)
	return d
}

func (t *Tally) lack(i int, empty []string) []string {
	t.missing[i]++
	return append(empty, t.bound[i].Key)
}

func (t *Tally) addLead(c Candidate) Decision {
	if c.Refusal != "" {
		t.out[c.Refusal]++
		return Decision{Outcome: OutcomeOut, Reason: c.Refusal}
	}
	values := make([]string, len(t.bound))
	var empty []string
	var excluded []campaign.MissingVariable
	undecided := false
	for i, b := range t.bound {
		value := ""
		if i < len(c.Values) {
			value = strings.TrimSpace(c.Values[i])
		}
		values[i] = value
		if value != "" || t.skipsSlot(i) {
			continue
		}
		empty = t.lack(i, empty)
		switch t.policyOf(b) {
		case MissingText:
			values[i] = t.textOf(b)
		case MissingExclude:
			excluded = append(excluded, campaign.MissingVariable{Slot: i + 1, Source: b.Source})
		default:
			undecided = true
		}
	}
	switch {
	case len(excluded) > 0:
		t.out[ReasonMissingVariable]++
		return Decision{Outcome: OutcomeOut, Reason: ReasonMissingVariable, Missing: excluded, Empty: empty}
	case undecided:
		t.undecided++
		return Decision{Outcome: OutcomeUndecided, Values: values, Empty: empty}
	}
	t.sendable++
	return Decision{Outcome: OutcomeSend, Values: values, Empty: empty}
}

func (t *Tally) policyOf(b Bound) MissingPolicy {
	if b.Missing == "" && !t.channel.RequiresChoice() {
		return MissingText
	}
	return b.Missing
}

func (t *Tally) textOf(b Bound) string {
	if b.Missing == MissingText {
		return b.Text
	}
	return b.Fallback
}

func (t *Tally) blockers() []Blocker {
	blockers := []Blocker{}
	if t.awaits != "" {
		blockers = append(blockers, t.awaits)
	}
	for i := range t.bound {
		if t.skipsSlot(i) {
			blockers = append(blockers, BlockerSourceRequired)
			break
		}
	}
	if t.undecided > 0 {
		blockers = append(blockers, BlockerChoiceRequired)
	}
	if t.source == SourceList {
		for _, n := range t.missing {
			if n > 0 {
				blockers = append(blockers, BlockerListVariablesMissing)
				break
			}
		}
	}
	if t.metadata.Any() {
		blockers = append(blockers, t.metadataBlocker())
	}
	return blockers
}

func (t *Tally) metadataBlocker() Blocker {
	if t.source == SourceList {
		return BlockerListMetadataMissing
	}
	return BlockerLeadsMetadataUnavailable
}

func (t *Tally) Creatable() error {
	for _, blocker := range t.blockers() {
		switch blocker {
		case BlockerTemplateRequired:
			return ErrTemplateRequired
		case BlockerMessageRequired:
			return ErrMessageRequired
		case BlockerSourceRequired:
			return ErrSourceRequired
		case BlockerChoiceRequired:
			return ErrChoiceRequired
		case BlockerListVariablesMissing:
			return ErrListVariablesMissing
		case BlockerListMetadataMissing:
			return ErrListMetadataMissing
		case BlockerLeadsMetadataUnavailable:
			return ErrLeadsMetadataUnavailable
		}
	}
	return nil
}

func (t *Tally) Count() Count {
	count := Count{
		Channel: t.channel, Source: t.source, Matched: t.matched, WithContact: t.contact,
		Out: map[string]int{}, RequiredVariables: len(t.bound), Variables: []VariableCount{}, Blockers: t.blockers(), MetadataMissing: t.metadata.Gaps(),
	}
	for reason, n := range t.out {
		count.Out[reason] = n
		count.OutTotal += n
	}
	for i, b := range t.bound {
		v := VariableCount{Key: b.Key, Name: b.Name, Header: b.Header, Source: b.Source, Value: b.Value, Missing: t.missing[i], Policy: b.Missing, Text: b.Text, Fallback: b.Fallback, Suggested: b.Suggested}
		v.ChoiceRequired = t.source == SourceLeads && t.channel.RequiresChoice() && b.Missing == "" && t.missing[i] > 0
		count.Variables = append(count.Variables, v)
	}
	undecidable := false
	for _, blocker := range count.Blockers {
		undecidable = undecidable || blocker == BlockerChoiceRequired || blocker == BlockerSourceRequired
	}
	if !undecidable {
		sendable := t.sendable
		count.Sendable = &sendable
	}
	return count
}

func (t *Tally) addRow(c Candidate) Decision {
	values := make([]string, len(t.bound))
	var empty []string
	for i := range t.bound {
		if i < len(c.Values) {
			values[i] = strings.TrimSpace(c.Values[i])
		}
		if values[i] == "" {
			empty = t.lack(i, empty)
		}
	}
	if c.Refusal != "" {
		t.out[c.Refusal]++
		return Decision{Outcome: OutcomeOut, Reason: c.Refusal, Values: values, Empty: empty}
	}
	t.sendable++
	return Decision{Outcome: OutcomeSend, Values: values, Empty: empty}
}
