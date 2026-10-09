package lead

import (
	"strings"
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/geo"
)

var importedAt = time.Date(2026, time.October, 8, 15, 0, 0, 0, time.UTC)

func rowsOf(numbers ...string) []ImportRow {
	rows := make([]ImportRow, 0, len(numbers))
	for i, number := range numbers {
		rows = append(rows, ImportRow{Line: i + 1, Number: number})
	}
	return rows
}

func prepare(rows []ImportRow, defs ...*customfield.Definition) PreparedImport {
	return PrepareImport("ws-1", rows, defs, importedAt)
}

func reasonsOf(issues []ImportIssue) map[RejectReason]ImportIssue {
	byReason := map[RejectReason]ImportIssue{}
	for _, issue := range issues {
		byReason[issue.Reason] = issue
	}
	return byReason
}

func TestPrepareImportNormalizesLocalNumbers(t *testing.T) {
	prepared := prepare(rowsOf("11987654321", "(11) 98765-4322", "+55 11 98765-4323"))

	if len(prepared.Issues) != 0 {
		t.Fatalf("issues = %+v, want none", prepared.Issues)
	}
	want := []string{"5511987654321", "5511987654322", "5511987654323"}
	if len(prepared.Records) != len(want) {
		t.Fatalf("records = %d, want %d", len(prepared.Records), len(want))
	}
	for i, number := range want {
		if prepared.Records[i].Number != number || prepared.Records[i].WorkspaceID != "ws-1" || !prepared.Records[i].At.Equal(importedAt) {
			t.Errorf("record[%d] = %+v, want number %q in ws-1", i, prepared.Records[i], number)
		}
	}
}

func TestPrepareImportRejectsUnreachableNumbers(t *testing.T) {
	prepared := prepare([]ImportRow{
		{Line: 1, Number: "5511987654321"},
		{Line: 2, Number: "not a phone", Name: "Ana"},
		{Line: 3, Number: "123", Name: "Bia"},
	})

	if len(prepared.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(prepared.Records))
	}
	if len(prepared.Issues) != 2 {
		t.Fatalf("issues = %+v, want 2", prepared.Issues)
	}
	for _, issue := range prepared.Issues {
		if issue.Reason != ReasonInvalid || !issue.Rejected || issue.Field != ImportFieldNumber {
			t.Errorf("issue = %+v, want a rejected invalid number", issue)
		}
	}
	if prepared.Issues[0].Line != 2 {
		t.Errorf("first rejection line = %d, want 2", prepared.Issues[0].Line)
	}
}

func TestPrepareImportDedupesWithinTheFileAcrossBatches(t *testing.T) {
	p := NewImportPreparer("ws-1", nil, importedAt)
	if _, issues, ok := p.Prepare(ImportRow{Line: 1, Number: "5511987654321"}); !ok || len(issues) != 0 {
		t.Fatalf("first sighting = %v, %+v", ok, issues)
	}
	_, issues, ok := p.Prepare(ImportRow{Line: 9, Number: "11987654321"})
	if ok || len(issues) != 1 || issues[0].Reason != ReasonDuplicate || issues[0].Line != 9 || !issues[0].Rejected {
		t.Fatalf("repeat = %v, %+v; want the repeat rejected as a duplicate", ok, issues)
	}
}

func TestPrepareImportCollapsesNinthDigitVariants(t *testing.T) {
	prepared := prepare(rowsOf("551187654321", "5511987654321"))

	if len(prepared.Records) != 1 {
		t.Fatalf("records = %d, want 1 (same person, two spellings)", len(prepared.Records))
	}
	if len(prepared.Issues) != 1 || prepared.Issues[0].Reason != ReasonDuplicate {
		t.Fatalf("issues = %+v, want one duplicate", prepared.Issues)
	}
}

func TestPrepareImportKeepsTheContactWhenTheNameIsUnusable(t *testing.T) {
	long := make([]byte, MaxLeadNameLength+10)
	for i := range long {
		long[i] = 'a'
	}
	prepared := prepare([]ImportRow{{Line: 1, Number: "5511987654321", Name: string(long)}})

	if len(prepared.Records) != 1 || prepared.Records[0].Name != "" {
		t.Fatalf("records = %+v, want the number kept without the name", prepared.Records)
	}
	if issue := reasonsOf(prepared.Issues)[ReasonNameTooLong]; issue.Line != 1 || issue.Rejected || issue.Field != ImportFieldName {
		t.Fatalf("issues = %+v, want the name reported and the row kept", prepared.Issues)
	}
}

func TestPrepareImportNormalizesNameWhitespace(t *testing.T) {
	prepared := prepare([]ImportRow{{Line: 1, Number: "5511987654321", Name: "  Ana   Maria  "}})

	if got := prepared.Records[0].Name; got != "Ana Maria" {
		t.Errorf("name = %q, want %q", got, "Ana Maria")
	}
}

func TestPrepareImportNeedsANumberOrAName(t *testing.T) {
	prepared := prepare([]ImportRow{
		{Line: 1, Name: "Bruna Souza", Phones: []ImportPhone{{Number: "1133334444", Label: PhoneLandline}}},
		{Line: 2, Phones: []ImportPhone{{Number: "1133334444", Label: PhoneLandline}}},
		{Line: 3, Name: "5511987654321"},
	})

	if len(prepared.Records) != 2 || prepared.Records[0].Line != 1 || prepared.Records[0].Number != "" {
		t.Fatalf("records = %+v, want the named row without WhatsApp kept", prepared.Records)
	}
	issue := reasonsOf(prepared.Issues)[ReasonIdentityRequired]
	if issue.Line != 2 || !issue.Rejected {
		t.Fatalf("issues = %+v, want line 2 rejected for having neither number nor name", prepared.Issues)
	}
}

func TestPrepareImportReadsEveryMappableField(t *testing.T) {
	interest := &customfield.Definition{Key: "interesse", Type: customfield.TypeSelect, Options: []string{"Matrícula", "Visita"}}
	score := &customfield.Definition{Key: "nota", Type: customfield.TypeNumber}
	since := &customfield.Definition{Key: "aluno_desde", Type: customfield.TypeDate}
	row := ImportRow{
		Line:      4,
		Number:    "11987654321",
		Name:      "Maria Aparecida Souza",
		Nickname:  "Cida",
		Email:     " Maria@Example.COM ",
		BirthDate: "12/03/1980",
		Phones: []ImportPhone{
			{Number: "(11) 3333-4444", Label: PhoneLandline},
			{Number: "11987654321", Label: PhoneMobile},
			{Number: "1133334444", Label: PhoneWork},
			{Number: "11 91234-5678", Label: PhoneMessage},
		},
		Postal:         address.Postal{ZipCode: "06404-000", Street: "R. das Acácias", Number: "12", District: "Jardim Silveira", City: "Barueri", State: "sp"},
		Latitude:       "-23,5105",
		Longitude:      "-46.8761",
		OwnerEmail:     " Clara@Escola.com.br ",
		ConsentDate:    "01/09/2026",
		ConsentPurpose: "Novidades da matrícula",
		CustomFields:   map[string]string{"interesse": "Matrícula", "nota": "8,5", "aluno_desde": "05/02/2020"},
		RelativeNumber: "(11) 98888-7777",
		RelationKind:   "Filha",
	}
	prepared := prepare([]ImportRow{row}, interest, score, since)
	if len(prepared.Issues) != 0 {
		t.Fatalf("issues = %+v, want none", prepared.Issues)
	}
	got := prepared.Records[0]
	if got.Number != "5511987654321" || got.Name != "Maria Aparecida Souza" || got.Nickname != "Cida" || got.Email != "maria@example.com" {
		t.Fatalf("identity fields = %+v", got)
	}
	if got.BirthDate == nil || got.BirthDate.String() != "1980-03-12" {
		t.Fatalf("birth date = %v", got.BirthDate)
	}
	if len(got.Phones) != 2 || got.Phones[0].Number != "551133334444" || got.Phones[0].Label != PhoneLandline ||
		got.Phones[1].Number != "5511912345678" || got.Phones[1].Label != PhoneMessage {
		t.Fatalf("phones = %+v, want the landline and the message phone without the identity or the repeat", got.Phones)
	}
	if got.Address == nil || !got.Address.Primary || got.Address.Label != AddressHome || got.Address.GeoStatus != GeoLocated {
		t.Fatalf("address = %+v", got.Address)
	}
	fix := got.Address.Fix
	if fix == nil || fix.Precision != geo.PrecisionExact || fix.Source != geo.SourceImport || fix.Point.Lat != -23.5105 || fix.Point.Lng != -46.8761 || !fix.FixedAt.Equal(importedAt) {
		t.Fatalf("fix = %+v, want an exact import fix", fix)
	}
	if got.Address.Postal.ZipCode != "06404000" || got.Address.Postal.State != "SP" {
		t.Fatalf("postal = %+v, want it normalized", got.Address.Postal)
	}
	if got.OwnerEmail != "clara@escola.com.br" {
		t.Fatalf("owner e-mail = %q", got.OwnerEmail)
	}
	consent := got.Consent
	if consent == nil || consent.Source != ConsentImport || consent.Purpose != "Novidades da matrícula" ||
		!consent.GrantedAt.Equal(time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("consent = %+v", consent)
	}
	if got.CustomFields["interesse"] != "Matrícula" || got.CustomFields["nota"] != 8.5 || got.CustomFields["aluno_desde"] != "2020-02-05" {
		t.Fatalf("custom fields = %#v", got.CustomFields)
	}
	if got.Relative == nil || got.Relative.Number != "5511988887777" || got.Relative.Kind != KindChild {
		t.Fatalf("relative = %+v", got.Relative)
	}
}

func TestPrepareImportWithoutCoordinatesLeavesTheAddressPending(t *testing.T) {
	prepared := prepare([]ImportRow{{Line: 1, Number: "5511987654321", Postal: address.Postal{District: "Centro", City: "Barueri", State: "SP"}}})
	got := prepared.Records[0].Address
	if got == nil || got.GeoStatus != GeoPending || got.Fix != nil {
		t.Fatalf("address = %+v, want a pending address for the sweeper", got)
	}
}

func TestPrepareImportTellsWhetherTheRowGaveAnAddress(t *testing.T) {
	cases := []struct {
		name   string
		row    ImportRow
		given  bool
		stored bool
	}{
		{"no address column filled", ImportRow{Line: 1, Number: "5511987654321"}, false, false},
		{"blank address cells", ImportRow{Line: 1, Number: "5511987654321", Postal: address.Postal{Street: "  ", City: " "}, Latitude: " "}, false, false},
		{"a usable address", ImportRow{Line: 1, Number: "5511987654321", Postal: address.Postal{City: "Barueri", State: "SP"}}, true, true},
		{"an address that cannot be used", ImportRow{Line: 1, Number: "5511987654321", Postal: address.Postal{Street: "Rua A", Number: "1"}}, true, false},
		{"coordinates alone", ImportRow{Line: 1, Number: "5511987654321", Latitude: "-23.5", Longitude: "-46.6"}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prepared := prepare([]ImportRow{tc.row})
			if len(prepared.Records) != 1 {
				t.Fatalf("records = %+v", prepared.Records)
			}
			got := prepared.Records[0]
			if got.AddressGiven != tc.given || (got.Address != nil) != tc.stored {
				t.Fatalf("given = %v, address = %+v; want given %v, stored %v", got.AddressGiven, got.Address, tc.given, tc.stored)
			}
		})
	}
}

func TestPrepareImportReadsAStateWrittenByName(t *testing.T) {
	prepared := prepare([]ImportRow{{Line: 1, Number: "5511987654321", Postal: address.Postal{City: "São Paulo", State: "São Paulo"}}})
	if len(prepared.Issues) != 0 || len(prepared.Records) != 1 {
		t.Fatalf("prepared = %+v, want the row accepted", prepared)
	}
	if got := prepared.Records[0].Address; got == nil || got.Postal.State != "SP" {
		t.Fatalf("address = %+v, want the state stored as its code", got)
	}
}

func TestPrepareImportKeepsAVeryLongCustomFieldKeyInItsIssue(t *testing.T) {
	key := strings.Repeat("k", 120)
	def := &customfield.Definition{Key: key, Type: customfield.TypeNumber}
	prepared := prepare([]ImportRow{{Line: 1, Number: "5511987654321", CustomFields: map[string]string{key: "muito"}}}, def)
	if len(prepared.Issues) != 1 || prepared.Issues[0].Field != "custom_field:"+key || prepared.Issues[0].Reason != ReasonCustomFieldInvalid {
		t.Fatalf("issues = %+v", prepared.Issues)
	}
}

func TestPrepareImportReportsEachBadFieldAndKeepsTheRow(t *testing.T) {
	score := &customfield.Definition{Key: "nota", Type: customfield.TypeNumber}
	longPurpose := make([]byte, MaxConsentPurposeLength+1)
	for i := range longPurpose {
		longPurpose[i] = 'x'
	}
	long := make([]byte, MaxLeadNameLength+1)
	for i := range long {
		long[i] = 'b'
	}
	base := func() ImportRow { return ImportRow{Line: 7, Number: "5511987654321", Name: "Ana"} }
	cases := []struct {
		name   string
		edit   func(r *ImportRow)
		reason RejectReason
		field  string
		check  func(t *testing.T, got ImportRecord)
	}{
		{"e-mail", func(r *ImportRow) { r.Email = "ana@" }, ReasonEmailInvalid, ImportFieldEmail, func(t *testing.T, got ImportRecord) {
			if got.Email != "" {
				t.Fatalf("email = %q", got.Email)
			}
		}},
		{"nickname", func(r *ImportRow) { r.Nickname = string(long) }, ReasonNicknameTooLong, ImportFieldNickname, nil},
		{"birth date in the future", func(r *ImportRow) { r.BirthDate = "01/01/2030" }, ReasonBirthDateInvalid, ImportFieldBirthDate, nil},
		{"birth date that is not a date", func(r *ImportRow) { r.BirthDate = "ontem" }, ReasonBirthDateInvalid, ImportFieldBirthDate, nil},
		{"contact phone", func(r *ImportRow) { r.Phones = []ImportPhone{{Number: "123", Label: PhoneMobile}} }, ReasonPhoneInvalid, ImportFieldPhones, func(t *testing.T, got ImportRecord) {
			if len(got.Phones) != 0 {
				t.Fatalf("phones = %+v", got.Phones)
			}
		}},
		{"address without a CEP or a city", func(r *ImportRow) { r.Postal = address.Postal{Street: "Rua A", Number: "1"} }, ReasonAddressInvalid, ImportFieldAddress, func(t *testing.T, got ImportRecord) {
			if got.Address != nil {
				t.Fatalf("address = %+v", got.Address)
			}
		}},
		{"coordinates without an address", func(r *ImportRow) { r.Latitude, r.Longitude = "-23.5", "-46.6" }, ReasonAddressInvalid, ImportFieldAddress, nil},
		{"coordinates that are not numbers", func(r *ImportRow) {
			r.Postal = address.Postal{City: "Barueri", State: "SP"}
			r.Latitude, r.Longitude = "norte", "-46.6"
		}, ReasonCoordinatesInvalid, ImportFieldCoordinates, func(t *testing.T, got ImportRecord) {
			if got.Address == nil || got.Address.Fix != nil || got.Address.GeoStatus != GeoPending {
				t.Fatalf("address = %+v, want it kept and pending", got.Address)
			}
		}},
		{"coordinates that are not finite", func(r *ImportRow) {
			r.Postal = address.Postal{City: "Barueri", State: "SP"}
			r.Latitude, r.Longitude = "NaN", "inf"
		}, ReasonCoordinatesInvalid, ImportFieldCoordinates, nil},
		{"only one coordinate", func(r *ImportRow) {
			r.Postal = address.Postal{City: "Barueri", State: "SP"}
			r.Latitude = "-23.5"
		}, ReasonCoordinatesInvalid, ImportFieldCoordinates, nil},
		{"coordinates outside Brazil", func(r *ImportRow) {
			r.Postal = address.Postal{City: "Barueri", State: "SP"}
			r.Latitude, r.Longitude = "40.7128", "-74.0060"
		}, ReasonCoordinatesOutsideBrazil, ImportFieldCoordinates, func(t *testing.T, got ImportRecord) {
			if got.Address == nil || got.Address.Fix != nil {
				t.Fatalf("address = %+v, want no fix", got.Address)
			}
		}},
		{"owner that is not an e-mail", func(r *ImportRow) { r.OwnerEmail = "Clara" }, ReasonOwnerNotFound, ImportFieldOwner, func(t *testing.T, got ImportRecord) {
			if got.OwnerEmail != "" {
				t.Fatalf("owner = %q", got.OwnerEmail)
			}
		}},
		{"consent date", func(r *ImportRow) { r.ConsentDate = "talvez" }, ReasonConsentDateInvalid, ImportFieldConsentDate, func(t *testing.T, got ImportRecord) {
			if got.Consent != nil {
				t.Fatalf("consent = %+v", got.Consent)
			}
		}},
		{"consent in the future", func(r *ImportRow) { r.ConsentDate = "01/01/2030" }, ReasonConsentDateInvalid, ImportFieldConsentDate, nil},
		{"consent purpose without a date", func(r *ImportRow) { r.ConsentPurpose = "Novidades" }, ReasonConsentDateInvalid, ImportFieldConsentDate, nil},
		{"consent purpose too long", func(r *ImportRow) {
			r.ConsentDate = "01/09/2026"
			r.ConsentPurpose = string(longPurpose)
		}, ReasonConsentPurposeTooLong, ImportFieldConsentPurpose, func(t *testing.T, got ImportRecord) {
			if got.Consent != nil {
				t.Fatalf("consent = %+v, want none recorded without its purpose", got.Consent)
			}
		}},
		{"custom value", func(r *ImportRow) { r.CustomFields = map[string]string{"nota": "muito boa"} }, ReasonCustomFieldInvalid, ImportCustomField("nota"), func(t *testing.T, got ImportRecord) {
			if len(got.CustomFields) != 0 {
				t.Fatalf("custom fields = %+v", got.CustomFields)
			}
		}},
		{"custom key the workspace does not have", func(r *ImportRow) { r.CustomFields = map[string]string{"cor": "azul"} }, ReasonCustomFieldInvalid, ImportCustomField("cor"), nil},
		{"custom number that is not a number", func(r *ImportRow) { r.CustomFields = map[string]string{"nota": "NaN"} }, ReasonCustomFieldInvalid, ImportCustomField("nota"), nil},
		{"custom number that is infinite", func(r *ImportRow) { r.CustomFields = map[string]string{"nota": "-Infinity"} }, ReasonCustomFieldInvalid, ImportCustomField("nota"), nil},
		{"relative number", func(r *ImportRow) { r.RelativeNumber = "12" }, ReasonRelativeNumberInvalid, ImportFieldRelative, func(t *testing.T, got ImportRecord) {
			if got.Relative != nil {
				t.Fatalf("relative = %+v", got.Relative)
			}
		}},
		{"relation kind", func(r *ImportRow) {
			r.RelativeNumber = "11988887777"
			r.RelationKind = "vizinho"
		}, ReasonRelationKindInvalid, ImportFieldRelationKind, nil},
		{"relative is the row itself", func(r *ImportRow) { r.RelativeNumber = "11987654321" }, ReasonRelationSelf, ImportFieldRelative, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := base()
			tc.edit(&row)
			prepared := prepare([]ImportRow{row}, score)
			if len(prepared.Records) != 1 {
				t.Fatalf("records = %+v, want the row kept", prepared.Records)
			}
			issue, ok := reasonsOf(prepared.Issues)[tc.reason]
			if !ok || issue.Line != 7 || issue.Rejected || issue.Field != tc.field {
				t.Fatalf("issues = %+v, want %s on %s for line 7", prepared.Issues, tc.reason, tc.field)
			}
			if tc.check != nil {
				tc.check(t, prepared.Records[0])
			}
		})
	}
}

func TestParseExistingPolicy(t *testing.T) {
	cases := []struct {
		in     string
		want   ExistingPolicy
		wantOK bool
	}{
		{"", PolicyFillEmpty, true},
		{"fill_empty", PolicyFillEmpty, true},
		{"skip", PolicySkip, true},
		{" skip ", PolicySkip, true},
		{"overwrite", "", false},
	}
	for _, c := range cases {
		got, ok := ParseExistingPolicy(c.in)
		if ok != c.wantOK || (ok && got != c.want) {
			t.Errorf("ParseExistingPolicy(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestParseRelationKind(t *testing.T) {
	cases := []struct {
		raw  string
		want RelationKind
		ok   bool
	}{
		{"", KindRelative, true},
		{"Filha", KindChild, true},
		{" filho ", KindChild, true},
		{"Mãe", KindParent, true},
		{"pai", KindParent, true},
		{"Esposa", KindSpouse, true},
		{"marido", KindSpouse, true},
		{"companheira", KindPartner, true},
		{"Irmão", KindSibling, true},
		{"avó", KindGrandparent, true},
		{"neto", KindGrandchild, true},
		{"tia", KindUncleAunt, true},
		{"sobrinho", KindNephewNiece, true},
		{"prima", KindCousin, true},
		{"sogra", KindInLaw, true},
		{"cunhado", KindInLaw, true},
		{"parente", KindRelative, true},
		{"child", KindChild, true},
		{"nephew_niece", KindNephewNiece, true},
		{"referred", "", false},
		{"vizinho", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseRelationKind(tc.raw)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseRelationKind(%q) = %q, %v; want %q, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestPrepareImportDedupesPeopleWithoutWhatsAppByNameAndPhone(t *testing.T) {
	landline := []ImportPhone{{Number: "1133334444", Label: PhoneLandline}}
	prepared := prepare([]ImportRow{
		{Line: 1, Name: "Bruna Souza", Phones: landline},
		{Line: 2, Name: "João Souza", Phones: landline},
		{Line: 3, Name: " bruna  SOUZA ", Phones: landline},
		{Line: 4, Name: "Bruna Souza"},
		{Line: 5, Name: "Bruna Souza"},
	})
	if len(prepared.Records) != 4 {
		t.Fatalf("records = %+v, want the repeat of line 1 dropped", prepared.Records)
	}
	issue := reasonsOf(prepared.Issues)[ReasonDuplicate]
	if issue.Line != 3 || !issue.Rejected || len(prepared.Issues) != 1 {
		t.Fatalf("issues = %+v, want line 3 rejected as a duplicate", prepared.Issues)
	}
}
