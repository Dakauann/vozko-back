package campaignaudience

import (
	"fmt"
	"strings"

	"vozko/domain/campaign"
	"vozko/domain/selection"
)

type Channel string

const (
	ChannelEmail      Channel = "email"
	ChannelWhatsApp   Channel = "whatsapp"
	ChannelUnofficial Channel = "unofficial"
	ChannelCarrier    Channel = "carrier"
)

func Channels() []Channel {
	return []Channel{ChannelEmail, ChannelWhatsApp, ChannelUnofficial, ChannelCarrier}
}

func (c Channel) Valid() bool {
	for _, known := range Channels() {
		if c == known {
			return true
		}
	}
	return false
}

func (c Channel) Contact() campaign.ContactKind {
	if c == ChannelEmail {
		return campaign.ContactEmail
	}
	return campaign.ContactPhone
}

func (c Channel) RequiresChoice() bool {
	return c == ChannelWhatsApp || c == ChannelUnofficial
}

func (c Channel) bindsSources() bool {
	return c.RequiresChoice()
}

type Source string

const (
	SourceLeads Source = "leads"
	SourceList  Source = "list"
)

type MissingPolicy string

const (
	MissingText    MissingPolicy = "text"
	MissingExclude MissingPolicy = "exclude"
)

func (p MissingPolicy) valid() bool {
	return p == "" || p == MissingText || p == MissingExclude
}

type Variable struct {
	Key     string                 `json:"key"`
	Source  campaign.BindingSource `json:"source,omitempty"`
	Value   string                 `json:"value,omitempty"`
	Missing MissingPolicy          `json:"missing,omitempty"`
	Text    string                 `json:"text,omitempty"`
}

func (v Variable) normalized() Variable {
	v.Key = strings.TrimSpace(v.Key)
	v.Source = campaign.BindingSource(strings.TrimSpace(string(v.Source)))
	v.Value = strings.TrimSpace(v.Value)
	v.Missing = MissingPolicy(strings.TrimSpace(string(v.Missing)))
	v.Text = strings.TrimSpace(v.Text)
	return v
}

type Spec struct {
	Source    Source
	Selection *selection.Selection
	ListID    string
	Variables []Variable
}

func (s Spec) Normalized() Spec {
	s.Source = Source(strings.TrimSpace(string(s.Source)))
	s.ListID = strings.TrimSpace(s.ListID)
	if s.Selection != nil {
		inferred := s.Selection.Inferred()
		s.Selection = &inferred
	}
	vars := make([]Variable, 0, len(s.Variables))
	for _, v := range s.Variables {
		vars = append(vars, v.normalized())
	}
	s.Variables = vars
	return s
}

func (s Spec) Validate() error {
	s = s.Normalized()
	switch s.Source {
	case SourceLeads:
		if s.ListID != "" {
			return ErrSourceAmbiguous
		}
		if s.Selection == nil {
			return ErrSelectionRequired
		}
		if err := s.Selection.ValidateForCount(); err != nil {
			return err
		}
	case SourceList:
		if s.Selection != nil {
			return ErrSourceAmbiguous
		}
		if s.ListID == "" {
			return ErrListRequired
		}
	default:
		return fmt.Errorf("%w: %q", ErrSourceInvalid, s.Source)
	}
	return validateVariables(s.Variables)
}

func (s Spec) ValidateForCreate() error {
	if err := s.Validate(); err != nil {
		return err
	}
	s = s.Normalized()
	if s.Source == SourceLeads {
		return s.Selection.ValidateConfirmed()
	}
	return nil
}

func ValidateVariables(vars []Variable) error {
	return validateVariables(NormalizeVariables(vars))
}

func NormalizeVariables(vars []Variable) []Variable {
	out := make([]Variable, 0, len(vars))
	for _, v := range vars {
		out = append(out, v.normalized())
	}
	return out
}

func validateVariables(vars []Variable) error {
	seen := map[string]bool{}
	for _, v := range vars {
		if v.Key == "" {
			return ErrVariableUnknown
		}
		if seen[v.Key] {
			return fmt.Errorf("%w: %q", ErrVariableDuplicate, v.Key)
		}
		seen[v.Key] = true
		if !v.Missing.valid() {
			return fmt.Errorf("%w: %q", ErrMissingPolicyInvalid, v.Missing)
		}
		if v.Missing == MissingText && v.Text == "" {
			return fmt.Errorf("%w: %q", ErrTextRequired, v.Key)
		}
	}
	return nil
}

type Slot struct {
	Key       string
	Name      string
	Header    bool
	Suggested campaign.BindingSource
	Fallback  string
	Example   string
}

type Bound struct {
	Variable
	Name      string
	Header    bool
	Suggested bool
	Fallback  string
}

func Bind(slots []Slot, vars []Variable) ([]Bound, error) {
	byKey := make(map[string]Variable, len(vars))
	for _, v := range vars {
		v = v.normalized()
		byKey[v.Key] = v
	}
	known := make(map[string]bool, len(slots))
	bound := make([]Bound, 0, len(slots))
	for _, slot := range slots {
		known[slot.Key] = true
		v, given := byKey[slot.Key]
		if !given {
			v = Variable{Key: slot.Key}
		}
		b := Bound{Variable: v, Name: slot.Name, Header: slot.Header, Fallback: slot.Fallback}
		if b.Source == "" && slot.Suggested != "" {
			b.Source, b.Suggested = slot.Suggested, true
		}
		bound = append(bound, b)
	}
	for key := range byKey {
		if !known[key] {
			return nil, fmt.Errorf("%w: %q", ErrVariableUnknown, key)
		}
	}
	return bound, nil
}

const unboundPlaceholder = "-"

func Unbound(b Bound) bool {
	return b.Source == ""
}

func Bindings(bound []Bound) []campaign.VariableBinding {
	out := make([]campaign.VariableBinding, 0, len(bound))
	for _, b := range bound {
		if Unbound(b) {
			out = append(out, campaign.VariableBinding{Source: campaign.BindLiteral, Value: unboundPlaceholder})
			continue
		}
		out = append(out, campaign.VariableBinding{Source: b.Source, Value: b.Value})
	}
	return out
}

func Fields(bound []Bound) []campaign.BindingSource {
	out := make([]campaign.BindingSource, 0, len(bound))
	for _, b := range bound {
		out = append(out, b.Source)
	}
	return out
}

type Request struct {
	Spec    Spec
	UserID  string
	IsAdmin bool
}
