package leadimport

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/sheet"
	"vozko/domain/unofficial_whatsapp"
	"vozko/domain/workspace"
)

var (
	interest  = &customfield.Definition{Key: "interesse", Label: "Interesse", Type: customfield.TypeText}
	stance    = &customfield.Definition{Key: "classificacao", Label: "Classificação", Type: customfield.TypeSelect, Options: []string{"Positivo"}, Sensitive: true}
	fieldDefs = []*customfield.Definition{interest, stance}
)

func fieldsOf(columns []Column) []string {
	out := make([]string, len(columns))
	for i, c := range columns {
		out[i] = c.Field
	}
	return out
}

func TestSuggestFindsAHomeForEveryCommonHeader(t *testing.T) {
	cases := []struct {
		name    string
		headers []string
		want    []string
	}{
		{
			name:    "the school network file",
			headers: []string{"telefone", "celular 2", "fone casa", "nome", "apelido", "data nasc", "cep", "endereco", "numero", "bairro", "cidade", "interesse", "familiar de", "parentesco"},
			want: []string{FieldNumber, FieldPhoneMobile, FieldPhoneLandline, FieldName, FieldNickname, FieldBirthDate, FieldZipCode, FieldStreet,
				FieldStreetNumber, FieldDistrict, FieldCity, lead.ImportCustomField("interesse"), FieldRelativeNumber, FieldRelationKind},
		},
		{
			name:    "a bare número is the WhatsApp when nothing else is",
			headers: []string{"Número", "Nome"},
			want:    []string{FieldNumber, FieldName},
		},
		{
			name:    "a número next to a phone column and an address is the house number",
			headers: []string{"WhatsApp", "Rua", "Número", "UF"},
			want:    []string{FieldNumber, FieldStreet, FieldStreetNumber, FieldState},
		},
		{
			name:    "e-mail, owner, consent and coordinates",
			headers: []string{"E-mail", "Responsável", "Data do consentimento", "Finalidade", "Latitude", "Longitude", "Complemento"},
			want:    []string{FieldEmail, FieldOwnerEmail, FieldConsentDate, FieldConsentPurpose, FieldLatitude, FieldLongitude, FieldComplement},
		},
		{
			name:    "custom fields match by key or label, sensitive ones included",
			headers: []string{"Classificação", "cupom"},
			want:    []string{lead.ImportCustomField("classificacao"), ""},
		},
		{
			name:    "at most four contact phones are suggested",
			headers: []string{"whatsapp", "telefone 2", "telefone 3", "telefone 4", "telefone 5", "telefone 6"},
			want:    []string{FieldNumber, FieldPhoneOther, FieldPhoneOther, FieldPhoneOther, FieldPhoneOther, ""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Suggest(tc.headers, fieldDefs)
			if !reflect.DeepEqual(fieldsOf(got), tc.want) {
				t.Fatalf("Suggest = %q, want %q", fieldsOf(got), tc.want)
			}
			for i, c := range got {
				if c.Index != i || c.Header != tc.headers[i] {
					t.Fatalf("column %d = %+v", i, c)
				}
			}
		})
	}
}

func settingsWith(fields ...string) Settings {
	columns := make([]Column, len(fields))
	for i, f := range fields {
		columns[i] = Column{Index: i, Field: f}
	}
	return Settings{Columns: columns, Policy: lead.PolicyFillEmpty}
}

func TestSettingsValidate(t *testing.T) {
	headers := []string{"a", "b", "c", "d", "e", "f"}
	script := &unofficial_whatsapp.SeedScript{Bodies: []string{"Oi {{1}}"}, MaxMessages: 4}
	cases := []struct {
		name     string
		settings Settings
		rule     string
	}{
		{"a number is enough", settingsWith(FieldNumber), ""},
		{"a name is enough", settingsWith(FieldName, FieldPhoneLandline), ""},
		{"ignored columns are fine", settingsWith(FieldNumber, ""), ""},
		{"neither number nor name", settingsWith(FieldEmail), RuleIdentityRequired},
		{"an unknown field", settingsWith(FieldNumber, "cpf"), RuleUnknownField},
		{"a custom key the workspace does not have", settingsWith(FieldNumber, lead.ImportCustomField("cor")), RuleUnknownField},
		{"a field twice", settingsWith(FieldNumber, FieldName, FieldName), RuleRepeated},
		{"five contact phones", settingsWith(FieldNumber, FieldPhoneMobile, FieldPhoneMobile, FieldPhoneWork, FieldPhoneOther, FieldPhoneMessage), RuleTooManyPhones},
		{"a column outside the file", Settings{Columns: []Column{{Index: 9, Field: FieldNumber}}, Policy: lead.PolicySkip}, RuleUnknownColumn},
		{"a column mapped twice", Settings{Columns: []Column{{Index: 0, Field: FieldNumber}, {Index: 0, Field: FieldName}}, Policy: lead.PolicySkip}, RuleRepeated},
		{"latitude without longitude", settingsWith(FieldNumber, FieldCity, FieldState, FieldLatitude), RuleNeedsPair},
		{"relation kind without the relative", settingsWith(FieldNumber, FieldRelationKind), RuleNeedsPair},
		{"consent purpose without the date", settingsWith(FieldNumber, FieldConsentPurpose), RuleNeedsPair},
		{"an unknown policy", Settings{Columns: []Column{{Index: 0, Field: FieldNumber}}, Policy: "overwrite"}, RulePolicy},
		{"a script without opening conversations", Settings{Columns: []Column{{Index: 0, Field: FieldNumber}}, Policy: lead.PolicySkip, Script: script}, RuleScript},
		{"a malformed script", Settings{Columns: []Column{{Index: 0, Field: FieldNumber}}, Policy: lead.PolicySkip, SeedInbox: true,
			Script: &unofficial_whatsapp.SeedScript{Bodies: []string{"Oi {{1}}", "Oi {{2}}"}, MaxMessages: 4}}, RuleScript},
		{"a script with its inbox seed", Settings{Columns: []Column{{Index: 0, Field: FieldNumber}}, Policy: lead.PolicySkip, SeedInbox: true, Script: script}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.settings.Validate(headers, fieldDefs)
			if tc.rule == "" {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			var mapping *MappingError
			if !errors.As(err, &mapping) || mapping.Rule != tc.rule || !errors.Is(err, ErrMappingInvalid) {
				t.Fatalf("Validate = %v, want rule %q", err, tc.rule)
			}
		})
	}
}

func TestSettingsRequirements(t *testing.T) {
	cases := []struct {
		name     string
		settings Settings
		want     []workspace.Action
	}{
		{"skip with plain fields needs only create", withPolicy(settingsWith(FieldNumber, FieldName, FieldEmail), lead.PolicySkip), []workspace.Action{workspace.ActionCreate}},
		{"filling existing leads needs update", settingsWith(FieldNumber), []workspace.Action{workspace.ActionCreate, workspace.ActionUpdate}},
		{"an owner column needs assign", withPolicy(settingsWith(FieldNumber, FieldOwnerEmail), lead.PolicySkip), []workspace.Action{workspace.ActionCreate, workspace.ActionAssign}},
		{"an address column needs the address permission", withPolicy(settingsWith(FieldNumber, FieldDistrict), lead.PolicySkip), []workspace.Action{workspace.ActionCreate, workspace.ActionReadAddresses}},
		{"coordinates need the address permission", withPolicy(settingsWith(FieldNumber, FieldLatitude, FieldLongitude), lead.PolicySkip), []workspace.Action{workspace.ActionCreate, workspace.ActionReadAddresses}},
		{"a sensitive column needs the sensitive permission", withPolicy(settingsWith(FieldNumber, lead.ImportCustomField("classificacao")), lead.PolicySkip), []workspace.Action{workspace.ActionCreate, workspace.ActionReadSensitive}},
		{"a plain custom column needs nothing more", withPolicy(settingsWith(FieldNumber, lead.ImportCustomField("interesse")), lead.PolicySkip), []workspace.Action{workspace.ActionCreate}},
		{"family columns need update", withPolicy(settingsWith(FieldNumber, FieldRelativeNumber), lead.PolicySkip), []workspace.Action{workspace.ActionCreate, workspace.ActionUpdate}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.settings.Requirements(fieldDefs); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Requirements = %v, want %v", got, tc.want)
			}
		})
	}
}

func withPolicy(s Settings, p lead.ExistingPolicy) Settings {
	s.Policy = p
	return s
}

func TestSettingsRowOfReadsTheMappedCells(t *testing.T) {
	s := Settings{Columns: []Column{
		{Index: 0, Field: FieldNumber}, {Index: 1, Field: FieldPhoneLandline}, {Index: 2, Field: FieldPhoneWork}, {Index: 3, Field: FieldName},
		{Index: 4, Field: FieldZipCode}, {Index: 5, Field: FieldStreetNumber}, {Index: 6, Field: FieldLatitude}, {Index: 7, Field: FieldLongitude},
		{Index: 8, Field: lead.ImportCustomField("interesse")}, {Index: 9, Field: FieldRelativeNumber}, {Index: 10, Field: FieldRelationKind},
		{Index: 11, Field: ""},
	}}
	row := sheet.Row{Line: 12, Cells: []string{"11987654321", "1133334444", "", "Maria", "06404-000", "12", "-23.5", "-46.8", "Matrícula", "11988887777", "filha", "lixo"}}
	got := s.RowOf(row)
	if got.Line != 12 || got.Number != "11987654321" || got.Name != "Maria" || got.Postal.ZipCode != "06404-000" || got.Postal.Number != "12" {
		t.Fatalf("row = %+v", got)
	}
	if len(got.Phones) != 2 || got.Phones[0] != (lead.ImportPhone{Number: "1133334444", Label: lead.PhoneLandline}) || got.Phones[1].Label != lead.PhoneWork {
		t.Fatalf("phones = %+v", got.Phones)
	}
	if got.Latitude != "-23.5" || got.Longitude != "-46.8" || got.CustomFields["interesse"] != "Matrícula" || got.RelativeNumber != "11988887777" || got.RelationKind != "filha" {
		t.Fatalf("row = %+v", got)
	}
	short := s.RowOf(sheet.Row{Line: 13, Cells: []string{"11987654321"}})
	if short.Number != "11987654321" || short.Name != "" {
		t.Fatalf("a short row = %+v", short)
	}
}

func TestFingerprintFollowsTheSettings(t *testing.T) {
	a := settingsWith(FieldNumber, FieldName)
	b := settingsWith(FieldNumber, FieldName)
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("the same settings must share a fingerprint")
	}
	b.Policy = lead.PolicySkip
	if a.Fingerprint() == b.Fingerprint() {
		t.Fatal("a different policy must change the fingerprint")
	}
	c := settingsWith(FieldNumber, FieldEmail)
	if a.Fingerprint() == c.Fingerprint() {
		t.Fatal("a different mapping must change the fingerprint")
	}
}

func TestCatalogListsEveryMappableField(t *testing.T) {
	fields := Catalog(fieldDefs)
	byKey := map[string]Field{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	for _, key := range []string{FieldNumber, FieldPhoneMobile, FieldPhoneLandline, FieldPhoneWork, FieldPhoneMessage, FieldPhoneOther, FieldName, FieldNickname,
		FieldEmail, FieldBirthDate, FieldZipCode, FieldStreet, FieldStreetNumber, FieldComplement, FieldDistrict, FieldCity, FieldState, FieldLatitude,
		FieldLongitude, FieldOwnerEmail, FieldConsentDate, FieldConsentPurpose, FieldRelativeNumber, FieldRelationKind,
		lead.ImportCustomField("interesse"), lead.ImportCustomField("classificacao")} {
		if _, ok := byKey[key]; !ok {
			t.Errorf("catalog misses %q", key)
		}
	}
	if f := byKey[lead.ImportCustomField("classificacao")]; f.Requires != workspace.ActionReadSensitive || !f.Sensitive || f.Label != "Classificação" {
		t.Errorf("sensitive field = %+v", f)
	}
	if f := byKey[FieldOwnerEmail]; f.Requires != workspace.ActionAssign {
		t.Errorf("owner field = %+v", f)
	}
	if f := byKey[FieldCity]; f.Requires != workspace.ActionReadAddresses || f.Group != GroupAddress {
		t.Errorf("city field = %+v", f)
	}
}
