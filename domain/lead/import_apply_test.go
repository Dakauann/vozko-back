package lead

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/geo"
	"vozko/domain/shared"
)

func importedRecord() ImportRecord {
	birth := shared.Date{Year: 1980, Month: time.March, Day: 12}
	return ImportRecord{
		WorkspaceID: "ws-1",
		At:          importedAt,
		Line:        3,
		Number:      "5511987654321",
		Name:        "Maria Souza",
		Nickname:    "Cida",
		Email:       "maria@example.com",
		BirthDate:   &birth,
		Phones:      []ContactPhone{{Number: "551133334444", Label: PhoneLandline}},
		Address:     &Address{Label: AddressHome, Primary: true, Postal: homePostal.Normalize(), GeoStatus: GeoPending},
		Owner:       "user-7",
		Consent:     &Consent{GrantedAt: importedAt, Source: ConsentImport, Purpose: "Novidades"},
		CustomFields: map[string]any{
			"interesse": "Matrícula",
		},
	}
}

func storedLead() *Lead {
	return &Lead{
		ID: "lead-1", WorkspaceID: "ws-1", Number: "5511987654321", Source: SourceChannel, Version: 4,
		Phones: []ContactPhone{}, Addresses: []Address{},
	}
}

func TestApplyImportCreatesALeadFromTheRow(t *testing.T) {
	got, err := ApplyImport(nil, importedRecord(), fillEmptyRules)
	if err != nil {
		t.Fatal(err)
	}
	l := got.Lead
	if got.Verdict != ImportCreated || l == nil {
		t.Fatalf("decision = %+v, want a created lead", got)
	}
	if l.WorkspaceID != "ws-1" || l.Source != SourceImport || l.NameSource != SourceImport || l.Name != "Maria Souza" || l.Version != 1 {
		t.Fatalf("lead = %+v", l)
	}
	if l.Owner != "user-7" || l.Email != "maria@example.com" || l.Nickname != "Cida" || l.BirthDate == nil {
		t.Fatalf("record fields = %+v", l)
	}
	if l.WhatsAppOptIn == nil || l.WhatsAppOptIn.Source != ConsentImport || l.WhatsAppOptIn.Purpose != "Novidades" {
		t.Fatalf("consent = %+v", l.WhatsAppOptIn)
	}
	if len(l.Phones) != 1 || len(l.Addresses) != 1 || !l.Addresses[0].Primary || l.CustomFields["interesse"] != "Matrícula" {
		t.Fatalf("collections = %+v %+v %+v", l.Phones, l.Addresses, l.CustomFields)
	}
	if l.Relations == nil {
		t.Fatal("a created lead carries empty collections so it can be saved")
	}
}

func TestApplyImportSkipChangesNothing(t *testing.T) {
	existing := storedLead()
	got, err := ApplyImport(existing, importedRecord(), ImportRules{Policy: PolicySkip})
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != ImportSkipped || got.Lead != nil || len(got.Conflicts) != 0 {
		t.Fatalf("decision = %+v, want skipped with nothing to write", got)
	}
}

func TestApplyImportRefusesALeadReadWithoutItsCollections(t *testing.T) {
	if _, err := ApplyImport(&Lead{ID: "lead-1", WorkspaceID: "ws-1", Number: "5511987654321"}, importedRecord(), fillEmptyRules); !errors.Is(err, ErrAggregateNotLoaded) {
		t.Fatalf("err = %v, want ErrAggregateNotLoaded", err)
	}
}

func TestApplyImportFillsOnlyWhatIsEmpty(t *testing.T) {
	manualFix := geo.Fix{Point: geo.Point{Lat: -23.56, Lng: -46.65}, Precision: geo.PrecisionExact, Source: geo.SourceManual, FixedAt: importedAt}
	optedOut := importedAt.Add(-time.Hour)
	cases := []struct {
		name      string
		existing  func(l *Lead)
		record    func(r *ImportRecord)
		verdict   ImportVerdict
		changed   []string
		conflicts []string
		check     func(t *testing.T, d ImportDecision)
	}{
		{
			name:     "an empty lead takes every field",
			existing: func(l *Lead) {},
			verdict:  ImportEnriched,
			changed:  []string{FieldAddresses, FieldBirthDate, FieldEmail, CustomFieldName("interesse"), FieldName, FieldNickname, FieldOwner, FieldPhones, FieldWhatsAppOptIn},
			check: func(t *testing.T, d ImportDecision) {
				if d.Lead.NameSource != SourceImport || d.Lead.Source != SourceChannel {
					t.Fatalf("lead = %+v, want an import name on a lead that keeps its own source", d.Lead)
				}
				if len(d.NewPhones) != 1 || d.NewAddress == nil || !d.NewAddress.Primary {
					t.Fatalf("additions = %+v %+v", d.NewPhones, d.NewAddress)
				}
			},
		},
		{
			name: "a manual name and other filled fields are kept and reported",
			existing: func(l *Lead) {
				l.Name, l.NameSource = "Maria Aparecida", SourceManual
				l.Email = "cida@example.com"
				l.Nickname = "Cidinha"
				birth := shared.Date{Year: 1981, Month: time.March, Day: 12}
				l.BirthDate = &birth
				l.Owner = "user-9"
				l.CustomFields = map[string]any{"interesse": "Visita"}
			},
			verdict:   ImportEnriched,
			changed:   []string{FieldAddresses, FieldPhones, FieldWhatsAppOptIn},
			conflicts: []string{FieldBirthDate, FieldEmail, CustomFieldName("interesse"), FieldName, FieldNickname, FieldOwner},
			check: func(t *testing.T, d ImportDecision) {
				if d.Lead.Name != "Maria Aparecida" || d.Lead.Email != "cida@example.com" || d.Lead.Owner != "user-9" || d.Lead.CustomFields["interesse"] != "Visita" {
					t.Fatalf("lead = %+v, want the stored values kept", d.Lead)
				}
			},
		},
		{
			name:     "a channel name is replaced by the import name",
			existing: func(l *Lead) { l.Name, l.NameSource = "Cida", SourceChannel },
			verdict:  ImportEnriched,
			changed:  []string{FieldAddresses, FieldBirthDate, FieldEmail, CustomFieldName("interesse"), FieldName, FieldNickname, FieldOwner, FieldPhones, FieldWhatsAppOptIn},
			check: func(t *testing.T, d ImportDecision) {
				if d.Lead.Name != "Maria Souza" || d.Lead.NameSource != SourceImport || slices.Contains(d.Conflicts, FieldName) {
					t.Fatalf("decision = %+v", d)
				}
			},
		},
		{
			name:     "a name from an earlier import is kept and reported",
			existing: func(l *Lead) { l.Name, l.NameSource = "Maria S.", SourceImport },
			verdict:  ImportEnriched,
			check: func(t *testing.T, d ImportDecision) {
				if d.Lead.Name != "Maria S." || !slices.Contains(d.Conflicts, FieldName) || slices.Contains(d.Changed, FieldName) {
					t.Fatalf("decision = %+v", d)
				}
			},
		},
		{
			name:     "a name that is only the lead's own number is filled",
			existing: func(l *Lead) { l.Name, l.NameSource = "+55 11 98765-4321", SourceManual },
			verdict:  ImportEnriched,
			check: func(t *testing.T, d ImportDecision) {
				if d.Lead.Name != "Maria Souza" || !slices.Contains(d.Changed, FieldName) || slices.Contains(d.Conflicts, FieldName) {
					t.Fatalf("decision = %+v", d)
				}
			},
		},
		{
			name: "an address that only had its city gets the rest from the file",
			existing: func(l *Lead) {
				l.Addresses = []Address{{ID: "a-1", Label: AddressHome, Primary: true, Postal: address.Postal{City: "São Paulo", State: "SP"}, GeoStatus: GeoApproximate,
					Fix: &geo.Fix{Point: geo.Point{Lat: -23.55, Lng: -46.63}, Precision: geo.PrecisionCity, Source: geo.SourceReference, FixedAt: importedAt}}}
			},
			verdict: ImportEnriched,
			check: func(t *testing.T, d ImportDecision) {
				if d.NewAddress != nil || d.FilledAddress == nil || len(d.Lead.Addresses) != 1 {
					t.Fatalf("decision = %+v, want the stored address filled", d)
				}
				filled := *d.FilledAddress
				if filled.ID != "a-1" || filled.Postal.Street != "Avenida Paulista" || filled.Postal.ZipCode != "01310100" || filled.GeoStatus != GeoPending || filled.Fix != nil {
					t.Fatalf("filled = %+v, want the street added and the address located again", filled)
				}
				if !slices.Contains(d.Changed, FieldAddresses) || slices.Contains(d.Conflicts, FieldAddresses) {
					t.Fatalf("decision = %+v", d)
				}
			},
		},
		{
			name: "coordinates from the file locate the address they fill",
			existing: func(l *Lead) {
				l.Addresses = []Address{{ID: "a-1", Label: AddressHome, Primary: true, Postal: address.Postal{City: "São Paulo", State: "SP"}, GeoStatus: GeoPending}}
			},
			record: func(r *ImportRecord) {
				fix := geo.Fix{Point: geo.Point{Lat: -23.5613, Lng: -46.6565}, Precision: geo.PrecisionExact, Source: geo.SourceImport, FixedAt: importedAt}
				r.Address = &Address{Label: AddressHome, Primary: true, Postal: homePostal.Normalize(), Fix: &fix, GeoStatus: GeoLocated}
			},
			verdict: ImportEnriched,
			check: func(t *testing.T, d ImportDecision) {
				if d.FilledAddress == nil || d.FilledAddress.Fix == nil || d.FilledAddress.Fix.Source != geo.SourceImport || d.FilledAddress.GeoStatus != GeoLocated {
					t.Fatalf("filled = %+v", d.FilledAddress)
				}
			},
		},
		{
			name:     "the same values are neither changes nor conflicts",
			existing: func(l *Lead) { l.Name, l.NameSource, l.Email = "Maria Souza", SourceManual, "maria@example.com" },
			record: func(r *ImportRecord) {
				*r = ImportRecord{WorkspaceID: "ws-1", At: importedAt, Line: 3, Number: r.Number, Name: "Maria Souza", Email: "maria@example.com"}
			},
			verdict: ImportUnchanged,
		},
		{
			name: "a stored address and pin are never replaced",
			existing: func(l *Lead) {
				l.Addresses = []Address{{ID: "a-1", Label: AddressHome, Primary: true, Postal: workPostal.Normalize(), Fix: &manualFix, GeoStatus: GeoLocated}}
			},
			verdict:   ImportEnriched,
			conflicts: []string{FieldAddresses},
			check: func(t *testing.T, d ImportDecision) {
				if d.NewAddress != nil || d.FilledAddress != nil || len(d.Lead.Addresses) != 1 || d.Lead.Addresses[0].Fix.Source != geo.SourceManual {
					t.Fatalf("decision = %+v", d)
				}
			},
		},
		{
			name: "the stored address is not a conflict",
			existing: func(l *Lead) {
				l.Addresses = []Address{{ID: "a-1", Label: AddressWork, Primary: true, Postal: homePostal.Normalize(), GeoStatus: GeoPending}}
			},
			verdict: ImportEnriched,
			check: func(t *testing.T, d ImportDecision) {
				if d.NewAddress != nil || d.FilledAddress != nil || slices.Contains(d.Conflicts, FieldAddresses) || slices.Contains(d.Changed, FieldAddresses) {
					t.Fatalf("decision = %+v", d)
				}
			},
		},
		{
			name: "phones already held are not added again, in either format",
			existing: func(l *Lead) {
				l.Phones = []ContactPhone{{ID: "p-1", Number: "551133334444", Label: PhoneLandline}}
			},
			record: func(r *ImportRecord) {
				r.Phones = []ContactPhone{{Number: "551133334444", Label: PhoneWork}, {Number: "5511912345678", Label: PhoneMobile}, {Number: "551187654321", Label: PhoneMobile}}
			},
			verdict: ImportEnriched,
			check: func(t *testing.T, d ImportDecision) {
				if len(d.NewPhones) != 1 || d.NewPhones[0].Number != "5511912345678" || len(d.Lead.Phones) != 2 {
					t.Fatalf("new phones = %+v, lead phones = %+v", d.NewPhones, d.Lead.Phones)
				}
			},
		},
		{
			name: "phones past the limit are reported, not added",
			existing: func(l *Lead) {
				for i := 0; i < MaxContactPhones-1; i++ {
					l.Phones = append(l.Phones, ContactPhone{ID: "p", Number: "55113333000" + string(rune('0'+i)), Label: PhoneOther})
				}
			},
			record: func(r *ImportRecord) {
				r.Phones = []ContactPhone{{Number: "5511912345678", Label: PhoneMobile}, {Number: "5511912345679", Label: PhoneMobile}}
			},
			verdict: ImportEnriched,
			check: func(t *testing.T, d ImportDecision) {
				if len(d.NewPhones) != 1 || len(d.Lead.Phones) != MaxContactPhones {
					t.Fatalf("new phones = %+v", d.NewPhones)
				}
				if len(d.Issues) != 1 || d.Issues[0].Reason != ReasonPhoneLimit || d.Issues[0].Line != 3 || d.Issues[0].Rejected {
					t.Fatalf("issues = %+v", d.Issues)
				}
			},
		},
		{
			name:      "a lead who opted out gets no consent from a file",
			existing:  func(l *Lead) { l.OptedOutAt = &optedOut },
			verdict:   ImportEnriched,
			conflicts: []string{FieldWhatsAppOptIn},
			check: func(t *testing.T, d ImportDecision) {
				if d.Lead.WhatsAppOptIn != nil || d.Lead.OptedOutAt == nil {
					t.Fatalf("lead = %+v", d.Lead)
				}
			},
		},
		{
			name:     "a consent already recorded is kept",
			existing: func(l *Lead) { l.WhatsAppOptIn = &Consent{GrantedAt: optedOut, Source: ConsentManual} },
			verdict:  ImportEnriched,
			check: func(t *testing.T, d ImportDecision) {
				if d.Lead.WhatsAppOptIn.Source != ConsentManual || slices.Contains(d.Conflicts, FieldWhatsAppOptIn) {
					t.Fatalf("decision = %+v", d)
				}
			},
		},
		{
			name: "a stored age is read, never written",
			existing: func(l *Lead) {
				age := 46
				l.Name, l.NameSource, l.StoredAge = "Maria Souza", SourceManual, &age
			},
			record: func(r *ImportRecord) {
				*r = ImportRecord{WorkspaceID: "ws-1", At: importedAt, Line: 3, Number: r.Number, Name: "Maria Souza", Email: "maria@example.com"}
			},
			verdict: ImportEnriched,
			changed: []string{FieldEmail},
			check: func(t *testing.T, d ImportDecision) {
				if d.Lead.StoredAge == nil || *d.Lead.StoredAge != 46 || slices.Contains(d.Changed, "age") {
					t.Fatalf("lead = %+v, changed = %v", d.Lead, d.Changed)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			existing := storedLead()
			tc.existing(existing)
			before := cloneForTest(existing)
			record := importedRecord()
			if tc.record != nil {
				tc.record(&record)
			}
			got, err := ApplyImport(existing, record, fillEmptyRules)
			if err != nil {
				t.Fatal(err)
			}
			if got.Verdict != tc.verdict {
				t.Fatalf("verdict = %s, want %s (%+v)", got.Verdict, tc.verdict, got)
			}
			if tc.changed != nil && !reflect.DeepEqual(got.Changed, sorted(tc.changed)) {
				t.Fatalf("changed = %v, want %v", got.Changed, tc.changed)
			}
			if tc.conflicts != nil && !reflect.DeepEqual(got.Conflicts, sorted(tc.conflicts)) {
				t.Fatalf("conflicts = %v, want %v", got.Conflicts, tc.conflicts)
			}
			if !reflect.DeepEqual(existing, before) {
				t.Fatalf("the stored lead was changed in place:\n got %+v\nwant %+v", existing, before)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func cloneForTest(l *Lead) *Lead {
	c := *l
	c.Phones = append([]ContactPhone{}, l.Phones...)
	c.Addresses = append([]Address{}, l.Addresses...)
	if l.CustomFields != nil {
		c.CustomFields = map[string]any{}
		for k, v := range l.CustomFields {
			c.CustomFields[k] = v
		}
	}
	return &c
}

func TestApplyImportRejectsARowThatCannotMakeALead(t *testing.T) {
	record := ImportRecord{WorkspaceID: "ws-1", At: importedAt, Line: 5, Address: &Address{Label: AddressHome, Primary: true, Postal: address.Postal{City: "Barueri", State: "SP"}, GeoStatus: GeoPending}}
	got, err := ApplyImport(nil, record, fillEmptyRules)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != ImportRejected || got.Lead != nil || len(got.Issues) != 1 || got.Issues[0].Reason != ReasonIdentityRequired || !got.Issues[0].Rejected {
		t.Fatalf("decision = %+v", got)
	}
}

func sorted(fields []string) []string {
	out := slices.Clone(fields)
	slices.Sort(out)
	return out
}

func TestMatchByContactFindsTheSamePersonWithoutWhatsApp(t *testing.T) {
	bruna := &Lead{ID: "lead-b", Name: "Bruna  Souza", Phones: []ContactPhone{{Number: "551133334444", Label: PhoneLandline}}, Addresses: []Address{}}
	joao := &Lead{ID: "lead-j", Name: "João Souza", Phones: []ContactPhone{{Number: "551133334444", Label: PhoneLandline}}, Addresses: []Address{}}
	record := ImportRecord{Name: "bruna souza", Phones: []ContactPhone{{Number: "551133334444", Label: PhoneLandline}}}
	cases := []struct {
		name    string
		record  ImportRecord
		holders []*Lead
		want    *Lead
	}{
		{"one holder with the same name", record, []*Lead{bruna, joao}, bruna},
		{"the same holder listed twice", record, []*Lead{bruna, bruna}, bruna},
		{"two holders with the same name are ambiguous", record, []*Lead{bruna, {ID: "lead-b2", Name: "Bruna Souza", Phones: bruna.Phones}}, nil},
		{"nobody with that name", ImportRecord{Name: "Ana", Phones: record.Phones}, []*Lead{bruna, joao}, nil},
		{"a holder without the phone", ImportRecord{Name: "Bruna Souza", Phones: []ContactPhone{{Number: "5511912345678"}}}, []*Lead{bruna}, nil},
		{"a row with WhatsApp is matched by its number elsewhere", ImportRecord{Number: "5511987654321", Name: "Bruna Souza", Phones: record.Phones}, []*Lead{bruna}, nil},
	}
	for _, tc := range cases {
		if got := MatchByContact(tc.record, tc.holders); got != tc.want {
			t.Errorf("%s: MatchByContact = %v, want %v", tc.name, got, tc.want)
		}
	}
}

var fillEmptyRules = ImportRules{Policy: PolicyFillEmpty}

func TestApplyImportReportsRequiredFieldsANewLeadMisses(t *testing.T) {
	defs := []*customfield.Definition{
		{Key: "interesse", Type: customfield.TypeText, Required: true},
		{Key: "turma", Type: customfield.TypeText, Required: true},
		{Key: "renda", Type: customfield.TypeNumber, Required: true, Sensitive: true, LegalBasis: "consentimento"},
		{Key: "cor", Type: customfield.TypeText},
	}
	cases := []struct {
		name   string
		viewer customfield.Viewer
		want   []string
	}{
		{"a viewer without sensitive data", customfield.Viewer{}, []string{ImportCustomField("turma")}},
		{"a viewer who reads sensitive data", customfield.Viewer{ReadsSensitive: true}, []string{ImportCustomField("renda"), ImportCustomField("turma")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ApplyImport(nil, importedRecord(), ImportRules{Policy: PolicyFillEmpty, Definitions: defs, Viewer: tc.viewer})
			if err != nil {
				t.Fatal(err)
			}
			if got.Verdict != ImportCreated {
				t.Fatalf("verdict = %s, a missing required field is reported and the lead is still created", got.Verdict)
			}
			var fields []string
			for _, issue := range got.Issues {
				if issue.Reason != ReasonCustomFieldRequired || issue.Line != 3 || issue.Rejected {
					t.Fatalf("issue = %+v", issue)
				}
				fields = append(fields, issue.Field)
			}
			if !reflect.DeepEqual(fields, tc.want) {
				t.Fatalf("fields = %v, want %v", fields, tc.want)
			}
		})
	}
	existing := storedLead()
	enriched, err := ApplyImport(existing, importedRecord(), ImportRules{Policy: PolicyFillEmpty, Definitions: defs})
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range enriched.Issues {
		if issue.Reason == ReasonCustomFieldRequired {
			t.Fatalf("an existing lead is not checked for required fields, got %+v", enriched.Issues)
		}
	}
}

func TestApplyImportKeepsAVeryLongCustomFieldKeyInItsIssue(t *testing.T) {
	key := strings.Repeat("k", 120)
	defs := []*customfield.Definition{{Key: key, Type: customfield.TypeText, Required: true}}
	record := importedRecord()
	record.CustomFields = nil
	got, err := ApplyImport(nil, record, ImportRules{Policy: PolicyFillEmpty, Definitions: defs})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Issues) != 1 || got.Issues[0].Field != "custom_field:"+key {
		t.Fatalf("issues = %+v", got.Issues)
	}
}
