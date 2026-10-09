package campaign

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/customfield"
	"vozko/domain/lead"
)

type BindingSource string

const (
	BindLiteral   BindingSource = "literal"
	BindFirstName BindingSource = "lead.first_name"
	BindName      BindingSource = "lead.name"
	BindNickname  BindingSource = "lead.nickname"
	BindDistrict  BindingSource = "lead.district"
	BindCity      BindingSource = "lead.city"
	BindOwnerName BindingSource = "lead.owner_name"
)

const customBindingPrefix = "lead.custom:"

var (
	ErrBindingsMismatch    = errors.New("campaign: every template variable needs exactly one binding")
	ErrBindingUnknown      = errors.New("campaign: the binding source is not one a variable can take")
	ErrBindingLiteralEmpty = errors.New("campaign: a fixed text variable cannot be empty")
	ErrBindingFieldUnknown = errors.New("campaign: the binding names a lead field that does not exist")
	ErrBindingSensitive    = errors.New("campaign: a sensitive lead field cannot be written into a message")
	ErrMissingVariable     = errors.New("campaign: a variable of this lead resolved empty")
)

func CustomBinding(key string) BindingSource {
	return BindingSource(customBindingPrefix + key)
}

type VariableBinding struct {
	Source BindingSource `json:"source"`
	Value  string        `json:"value,omitempty"`
}

type BindingSubject struct {
	Lead      *lead.Lead
	OwnerName string
}

type boundSlot struct {
	source BindingSource
	text   string
	key    string
}

type BindingPlan struct {
	slots []boundSlot
}

func PlanBindings(bindings []VariableBinding, slots int, defs []*customfield.Definition) (BindingPlan, error) {
	if len(bindings) != slots {
		return BindingPlan{}, fmt.Errorf("%w: %d variables, %d bindings", ErrBindingsMismatch, slots, len(bindings))
	}
	plan := BindingPlan{slots: make([]boundSlot, 0, len(bindings))}
	for i, b := range bindings {
		slot, err := planSlot(b, defs)
		if err != nil {
			return BindingPlan{}, fmt.Errorf("variable {{%d}}: %w", i+1, err)
		}
		plan.slots = append(plan.slots, slot)
	}
	return plan, nil
}

func planSlot(b VariableBinding, defs []*customfield.Definition) (boundSlot, error) {
	source := BindingSource(strings.TrimSpace(string(b.Source)))
	if source == BindLiteral {
		text := strings.TrimSpace(b.Value)
		if text == "" {
			return boundSlot{}, ErrBindingLiteralEmpty
		}
		return boundSlot{source: source, text: text}, nil
	}
	if strings.TrimSpace(b.Value) != "" {
		return boundSlot{}, fmt.Errorf("%w: only a fixed text carries a value", ErrBindingUnknown)
	}
	if key, custom := strings.CutPrefix(string(source), customBindingPrefix); custom {
		return planCustomSlot(strings.TrimSpace(key), defs)
	}
	switch source {
	case BindFirstName, BindName, BindNickname, BindDistrict, BindCity, BindOwnerName:
		return boundSlot{source: source}, nil
	}
	return boundSlot{}, fmt.Errorf("%w: %q", ErrBindingUnknown, b.Source)
}

func planCustomSlot(key string, defs []*customfield.Definition) (boundSlot, error) {
	for _, def := range defs {
		if def == nil || key == "" || def.Key != key {
			continue
		}
		if def.Sensitive {
			return boundSlot{}, fmt.Errorf("%w: %q", ErrBindingSensitive, key)
		}
		return boundSlot{source: CustomBinding(key), key: key}, nil
	}
	return boundSlot{}, fmt.Errorf("%w: %q", ErrBindingFieldUnknown, key)
}

func (p BindingPlan) Slots() int {
	return len(p.slots)
}

func (p BindingPlan) NeedsLead() bool {
	for _, s := range p.slots {
		if s.source != BindLiteral {
			return true
		}
	}
	return false
}

func (p BindingPlan) NeedsAddresses() bool {
	return p.uses(BindDistrict) || p.uses(BindCity)
}

func (p BindingPlan) NeedsOwnerNames() bool {
	return p.uses(BindOwnerName)
}

func (p BindingPlan) uses(source BindingSource) bool {
	for _, s := range p.slots {
		if s.source == source {
			return true
		}
	}
	return false
}

func (p BindingPlan) Resolve(subject BindingSubject) ([]string, error) {
	values := make([]string, 0, len(p.slots))
	var missing []MissingVariable
	for i, s := range p.slots {
		value := strings.TrimSpace(s.value(subject))
		if value == "" {
			missing = append(missing, MissingVariable{Slot: i + 1, Source: s.source})
		}
		values = append(values, value)
	}
	if len(missing) > 0 {
		return nil, &MissingVariableError{Missing: missing}
	}
	return values, nil
}

func (s boundSlot) value(subject BindingSubject) string {
	if s.source == BindLiteral {
		return s.text
	}
	l := subject.Lead
	if l == nil {
		return ""
	}
	switch s.source {
	case BindFirstName:
		first, _ := l.SplitName()
		return first
	case BindName:
		return l.RealName()
	case BindNickname:
		return lead.NormalizeName(l.Nickname)
	case BindDistrict:
		if primary := l.PrimaryAddress(); primary != nil {
			return primary.Postal.District
		}
	case BindCity:
		if primary := l.PrimaryAddress(); primary != nil {
			return primary.Postal.City
		}
	case BindOwnerName:
		return lead.NormalizeName(subject.OwnerName)
	default:
		return customfield.FormatValueJoined(l.CustomFields[s.key], ", ")
	}
	return ""
}
