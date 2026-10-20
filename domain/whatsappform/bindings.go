package whatsappform

import (
	"slices"

	"vozko/domain/customfield"
)

type BindingTarget string

const (
	BindName            BindingTarget = "name"
	BindEmail           BindingTarget = "email"
	BindBirthDate       BindingTarget = "birth_date"
	BindAddressZip      BindingTarget = "address.zip"
	BindAddressCity     BindingTarget = "address.city"
	BindAddressState    BindingTarget = "address.state"
	BindCustomField     BindingTarget = "custom_field"
	BindWhatsAppConsent BindingTarget = "consent_whatsapp"
)

type Binding struct {
	Target         BindingTarget `json:"target"`
	CustomFieldKey string        `json:"customFieldKey,omitempty"`
}

var leadTargets = map[BindingTarget][]FieldKind{
	BindName:            {FieldShortText},
	BindEmail:           {FieldEmail},
	BindBirthDate:       {FieldDate},
	BindAddressZip:      {FieldShortText},
	BindAddressCity:     {FieldShortText, FieldRadio, FieldDropdown},
	BindAddressState:    {FieldShortText, FieldRadio, FieldDropdown},
	BindWhatsAppConsent: {FieldOptIn},
}

var customFieldKinds = map[customfield.FieldType][]FieldKind{
	customfield.TypeText: {FieldShortText, FieldEmail, FieldPhone, FieldNumber, FieldLongText,
		FieldRadio, FieldDropdown, FieldCheckbox, FieldMultiChips, FieldDate, FieldDateRange},
	customfield.TypeNumber:      {FieldNumber},
	customfield.TypeDate:        {FieldDate},
	customfield.TypeBoolean:     {FieldOptIn},
	customfield.TypeSelect:      {FieldRadio, FieldDropdown},
	customfield.TypeMultiSelect: {FieldCheckbox, FieldMultiChips},
}

func (b Binding) slot() string {
	if b.Target == BindCustomField {
		return string(b.Target) + ":" + b.CustomFieldKey
	}
	return string(b.Target)
}

func Validate(doc Document, defs []*customfield.Definition) Problems {
	return append(ValidateStructure(doc), CheckBindings(doc, defs)...)
}

func CheckBindings(doc Document, defs []*customfield.Definition) Problems {
	index := map[string]*customfield.Definition{}
	for _, d := range defs {
		if d != nil && d.ObjectType == customfield.ObjectLead {
			index[d.Key] = d
		}
	}
	var problems Problems
	used := map[string]bool{}
	for _, s := range doc.Screens {
		for _, b := range s.Blocks {
			if b.Field == nil || b.Field.Binding == nil {
				continue
			}
			if code := bindingProblem(*b.Field, index); code != "" {
				problems = append(problems, Problem{Screen: s.Key, Block: b.Key, Property: "binding", Code: code})
				continue
			}
			slot := b.Field.Binding.slot()
			if used[slot] {
				problems = append(problems, Problem{Screen: s.Key, Block: b.Key, Property: "binding", Code: ProblemBindingDuplicate})
			}
			used[slot] = true
		}
	}
	return problems
}

func bindingProblem(f Field, defs map[string]*customfield.Definition) string {
	binding := *f.Binding
	if f.Kind.IsMedia() {
		return ProblemBindingKind
	}
	if f.IsSensitive() {
		return ProblemBindingSensitive
	}
	if binding.Target != BindCustomField {
		kinds, ok := leadTargets[binding.Target]
		switch {
		case !ok:
			return ProblemInvalid
		case !slices.Contains(kinds, f.Kind):
			return ProblemBindingKind
		}
		return ""
	}
	def, ok := defs[binding.CustomFieldKey]
	if !ok {
		return ProblemBindingUnknown
	}
	if def.Sensitive {
		return ProblemBindingSensitive
	}
	if !slices.Contains(customFieldKinds[def.Type], f.Kind) {
		return ProblemBindingKind
	}
	if def.Type == customfield.TypeSelect || def.Type == customfield.TypeMultiSelect {
		for _, o := range f.Options {
			if !slices.Contains(def.Options, o.Title) {
				return ProblemBindingOptions
			}
		}
	}
	return ""
}

func BindableKinds() map[BindingTarget][]FieldKind {
	out := make(map[BindingTarget][]FieldKind, len(leadTargets))
	for target, kinds := range leadTargets {
		out[target] = slices.Clone(kinds)
	}
	return out
}

func CustomFieldKinds(t customfield.FieldType) []FieldKind {
	return slices.Clone(customFieldKinds[t])
}
