package customfield

import (
	"errors"
	"strings"
	"testing"
)

func leadClassification() *Definition {
	return &Definition{
		WorkspaceID: "ws1",
		ObjectType:  ObjectLead,
		Key:         "classificacao",
		Label:       "Classificação",
		Type:        TypeSelect,
		Options:     []string{"Positivo", "Negativo", "A conquistar", "Não informado"},
		Sensitive:   true,
		LegalBasis:  "Consentimento para comunicação política",
		OptionTones: map[string]Tone{"Positivo": ToneChart1, "Negativo": ToneChart3, "A conquistar": ToneChart2, "Não informado": ToneNeutral},
		Role:        RoleClassification,
	}
}

func TestObjectTypeAcceptsOnlyKnownObjects(t *testing.T) {
	cases := []struct {
		object ObjectType
		valid  bool
	}{
		{ObjectOpportunity, true},
		{ObjectLead, true},
		{"conversation", false},
		{"", false},
		{"Lead", false},
	}
	for _, tc := range cases {
		if got := tc.object.Valid(); got != tc.valid {
			t.Errorf("ObjectType(%q).Valid() = %v, want %v", tc.object, got, tc.valid)
		}
	}
}

func TestOnlyLeadFieldsAskForAnExplicitSensitivityChoice(t *testing.T) {
	if !ObjectLead.RequiresSensitivityChoice() {
		t.Fatal("a lead field must be created with an explicit sensitive choice")
	}
	if ObjectOpportunity.RequiresSensitivityChoice() {
		t.Fatal("opportunity fields keep the non-sensitive default")
	}
}

func TestDefinitionValidateRules(t *testing.T) {
	if err := leadClassification().Validate(); err != nil {
		t.Fatalf("the classification preset must be valid, got %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Definition)
		want   error
	}{
		{"unknown object", func(d *Definition) { d.ObjectType = "conversation" }, ErrInvalidObjectType},
		{"missing object", func(d *Definition) { d.ObjectType = "" }, ErrInvalidObjectType},
		{"sensitive without legal basis", func(d *Definition) { d.LegalBasis = "  " }, ErrLegalBasisRequired},
		{"legal basis too long", func(d *Definition) { d.LegalBasis = strings.Repeat("a", MaxLegalBasisLength+1) }, ErrLegalBasisTooLong},
		{"free colour tone", func(d *Definition) { d.OptionTones["Positivo"] = "#00ff00" }, ErrInvalidTone},
		{"tone on a missing option", func(d *Definition) { d.OptionTones["Indeciso"] = ToneChart4 }, ErrToneUnknownOption},
		{"tones on a text field", func(d *Definition) {
			d.Type = TypeText
			d.Options = nil
			d.Role = ""
		}, ErrToneUnknownOption},
		{"a sensitive opportunity field", func(d *Definition) {
			d.ObjectType = ObjectOpportunity
			d.Role = ""
		}, ErrSensitiveUnsupportedObject},
		{"unknown role", func(d *Definition) { d.Role = "priority" }, ErrInvalidRole},
		{"classification on a multiselect", func(d *Definition) { d.Type = TypeMultiSelect }, ErrRoleRequiresSelect},
		{"classification on a number", func(d *Definition) {
			d.Type = TypeNumber
			d.Options = nil
			d.OptionTones = nil
		}, ErrRoleRequiresSelect},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := leadClassification()
			tc.mutate(d)
			if err := d.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
			if !errors.Is(d.Validate(), ErrInvalidDefinition) {
				t.Fatalf("%v must read as an invalid definition", tc.want)
			}
		})
	}
}

func TestEveryToneKeyIsAllowed(t *testing.T) {
	for _, tone := range []Tone{ToneChart1, ToneChart2, ToneChart3, ToneChart4, ToneChart5, ToneNeutral} {
		if !tone.Valid() {
			t.Errorf("tone %q must be valid", tone)
		}
	}
	for _, tone := range []Tone{"", "chart-6", "green", "#ff0000"} {
		if tone.Valid() {
			t.Errorf("tone %q must be refused", tone)
		}
	}
}

func TestNormalizeClearsTheLegalBasisOfAPlainField(t *testing.T) {
	d := leadClassification()
	d.Sensitive = false
	d.LegalBasis = "consent"
	d.Normalize()
	if d.LegalBasis != "" {
		t.Fatalf("LegalBasis = %q, want it cleared when the field is not sensitive", d.LegalBasis)
	}

	s := leadClassification()
	s.LegalBasis = "  consent  "
	s.Normalize()
	if s.LegalBasis != "consent" {
		t.Fatalf("LegalBasis = %q, want it trimmed", s.LegalBasis)
	}
}

func TestNormalizeLowercasesTheObjectType(t *testing.T) {
	d := leadClassification()
	d.ObjectType = " Lead "
	d.Normalize()
	if d.ObjectType != ObjectLead {
		t.Fatalf("ObjectType = %q, want %q", d.ObjectType, ObjectLead)
	}
}

func TestReplaceOptionsDropsTonesOfRemovedOptions(t *testing.T) {
	d := leadClassification()
	d.ReplaceOptions([]string{"Positivo", "Negativo"})

	if len(d.Options) != 2 {
		t.Fatalf("Options = %v", d.Options)
	}
	if _, kept := d.OptionTones["A conquistar"]; kept {
		t.Fatal("a removed option must lose its tone")
	}
	if d.OptionTones["Positivo"] != ToneChart1 {
		t.Fatalf("a kept option must keep its tone, got %v", d.OptionTones)
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("the definition must stay valid after replacing options: %v", err)
	}
}

func TestVisibleTo(t *testing.T) {
	plain := &Definition{Key: "origem"}
	sensitive := &Definition{Key: "classificacao", Sensitive: true}

	cases := []struct {
		name   string
		def    *Definition
		viewer Viewer
		want   bool
	}{
		{"plain field, any viewer", plain, Viewer{}, true},
		{"sensitive field, viewer without the permission", sensitive, Viewer{}, false},
		{"sensitive field, viewer with the permission", sensitive, Viewer{ReadsSensitive: true}, true},
		{"missing definition", nil, Viewer{ReadsSensitive: true}, false},
	}
	for _, tc := range cases {
		if got := VisibleTo(tc.def, tc.viewer); got != tc.want {
			t.Errorf("%s: VisibleTo() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestEveryDefinitionRefusalReadsAsAnInvalidDefinition(t *testing.T) {
	refusals := []error{
		ErrWorkspaceRequired, ErrInvalidObjectType, ErrKeyRequired, ErrLabelRequired, ErrInvalidType,
		ErrOptionsRequired, ErrSensitivityChoiceMissing, ErrLegalBasisRequired, ErrLegalBasisTooLong,
		ErrSensitiveUnsupportedObject, ErrInvalidTone, ErrToneUnknownOption, ErrInvalidRole, ErrRoleRequiresSelect,
	}
	for _, refusal := range refusals {
		if !errors.Is(refusal, ErrInvalidDefinition) {
			t.Errorf("%v must wrap ErrInvalidDefinition", refusal)
		}
	}
	for _, other := range []error{ErrNotFound, ErrKeyExists, ErrRoleTaken, ErrDefinitionsUnavailable, ErrValueType} {
		if errors.Is(other, ErrInvalidDefinition) {
			t.Errorf("%v is not a definition refusal", other)
		}
	}
}

func TestAPlainOpportunityFieldStaysValid(t *testing.T) {
	d := &Definition{WorkspaceID: "ws1", ObjectType: ObjectOpportunity, Key: "score", Label: "Score", Type: TypeNumber}
	if err := d.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}
