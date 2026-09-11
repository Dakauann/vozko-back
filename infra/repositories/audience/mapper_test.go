package audience_repository

import (
	"reflect"
	"testing"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
)

// The mapper must carry EVERY field, and this test is structural rather than a
// list of assertions on purpose.
//
// A field added to the entity and forgotten in fromDomain/toDomain is the
// quietest bug in this layer: nothing fails, the column simply stays at its
// zero value and the label the customer paid a model to produce is dropped on
// the way to the database. Listing the fields by hand here would have the same
// flaw, since the list would need the same edit. Walking the struct by
// reflection means a new field fails this test until it is mapped.
func TestMapperCarriesEveryField(t *testing.T) {
	original := fullyPopulatedAnalysis()

	roundTripped := toDomain(fromDomain(original))
	if roundTripped == nil {
		t.Fatal("toDomain(fromDomain(x)) returned nil")
	}

	got := reflect.ValueOf(*roundTripped)
	want := reflect.ValueOf(*original)
	typ := got.Type()

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		g, w := got.Field(i).Interface(), want.Field(i).Interface()
		if !reflect.DeepEqual(g, w) {
			t.Errorf("field %s did not survive the round trip: got %#v, want %#v", field.Name, g, w)
		}
	}
}

// Every field is set to something distinguishable from the zero value, so a
// field the mapper drops shows up as a difference rather than matching by
// accident. Reflection checks that this fixture itself stays complete: a new
// field left at its zero value here would make the test above vacuous for it.
func fullyPopulatedAnalysis() *ca.Analysis {
	now := time.Date(2026, 4, 5, 10, 30, 0, 0, time.UTC)
	later := now.Add(time.Hour)
	parent := "parent-comment-1"

	return &ca.Analysis{
		ID:               "11111111-1111-1111-1111-111111111111",
		WorkspaceID:      "22222222-2222-2222-2222-222222222222",
		SubjectKind:      ca.SubjectKindConversation,
		Source:           ca.SourceWhatsApp,
		AccountID:        "33333333-3333-3333-3333-333333333333",
		ContainerID:      "campaign-1",
		SubjectID:        "entry-1",
		ParentSubjectID:  &parent,
		AuthorExternalID: "author-1",
		AuthorHandle:     "fulano",
		Status:           ca.StatusAnalyzed,
		Attempts:         2,
		FailureReason:    "provider_error",

		Sentiment: shared.SentimentNegative,
		Stance:    ca.StanceCritic,
		Intent:    ca.IntentComplaint,
		TopicKey:  "atendimento",
		IsSpam:    true,
		Language:  "pt",

		Toxicity:       shared.QualityLevelMedium,
		PersonalAttack: shared.QualityLevelLow,
		LegalRisk:      shared.QualityLevelHigh,
		Severity:       73,

		Interest:          ca.InterestInterested,
		ProductInterest:   "plano familia",
		Disposition:       ca.DispositionFillingInfo,
		Qualification:     ca.QualificationHotLead,
		NextAction:        ca.NextActionEscalate,
		Summary:           "Cliente pediu orcamento e enviou documentos.",
		AttendanceQuality: 88,
		MessageCount:      17,

		RequiresAction: true,
		Excerpt:        "trecho do assunto",
		Truncated:      true,

		BatchID:    "44444444-4444-4444-4444-444444444444",
		Model:      "openai/gpt-4o-mini",
		AnalyzedAt: &later,
		OccurredAt: now,
		CreatedAt:  now,
		UpdatedAt:  later,
		DeletedAt:  &later,
	}
}

// Guards the fixture above: every exported field must be non-zero, otherwise
// the round-trip test silently stops covering it.
func TestMapperFixtureLeavesNoFieldZero(t *testing.T) {
	v := reflect.ValueOf(*fullyPopulatedAnalysis())
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		if v.Field(i).IsZero() {
			t.Errorf("fixture leaves %s at its zero value, so the round-trip test does not cover it", field.Name)
		}
	}
}

// A row stored before conversations existed has no subject kind. It must read
// back as a comment rather than as an unknown kind, and it must WRITE as
// 'comment' rather than as an empty string into a NOT NULL column.
func TestMapperDefaultsTheSubjectKind(t *testing.T) {
	a := fullyPopulatedAnalysis()
	a.SubjectKind = ""

	if got := fromDomain(a).SubjectKind; got != string(ca.SubjectKindComment) {
		t.Errorf("fromDomain wrote subject kind %q, want comment", got)
	}

	row := fromDomain(fullyPopulatedAnalysis())
	row.SubjectKind = ""
	if got := toDomain(row).Kind(); got != ca.SubjectKindComment {
		t.Errorf("a row with no subject kind read back as %q, want comment", got)
	}
}

func TestMapperHandlesNilBatchAndOptionalPointers(t *testing.T) {
	a := fullyPopulatedAnalysis()
	a.BatchID = ""
	a.ParentSubjectID = nil
	a.AnalyzedAt = nil
	a.DeletedAt = nil

	row := fromDomain(a)
	if row.BatchID != nil {
		t.Errorf("an empty batch id must map to NULL, got %v", *row.BatchID)
	}

	back := toDomain(row)
	if back.BatchID != "" || back.ParentSubjectID != nil || back.AnalyzedAt != nil || back.DeletedAt != nil {
		t.Errorf("optional fields did not round-trip as empty: %+v", back)
	}
}

func TestToDomainNilIsNil(t *testing.T) {
	if toDomain(nil) != nil {
		t.Error("toDomain(nil) should be nil")
	}
}

// The counters scan row must cover every field of the domain's Counters, and
// the mapping between them must carry all of them.
//
// This is the same class of silent bug the mapper test guards: a counter added
// to the domain but not to CountersRow, or to CountersRow but not to counters(),
// reads as a permanent zero. On a dashboard a zero is indistinguishable from
// "there were none", so nothing ever looks broken.
func TestCountersRowCoversEveryDomainCounter(t *testing.T) {
	row := reflect.TypeOf(CountersRow{})
	inRow := map[string]bool{}
	for i := 0; i < row.NumField(); i++ {
		inRow[row.Field(i).Name] = true
	}

	counters := reflect.TypeOf(ca.Counters{})
	for i := 0; i < counters.NumField(); i++ {
		name := counters.Field(i).Name
		// FlaggedAuthors comes from the author projection, not from this query.
		if name == "FlaggedAuthors" {
			continue
		}
		if !inRow[name] {
			t.Errorf("Counters.%s has no column in CountersRow, so it is always zero", name)
		}
	}
}

// Every field CountersRow scans must reach the domain. Filling the row with
// distinguishable values and asserting none arrives zero catches a field
// dropped from counters().
func TestCountersMappingCarriesEveryField(t *testing.T) {
	var rowValue CountersRow
	v := reflect.ValueOf(&rowValue).Elem()
	for i := 0; i < v.NumField(); i++ {
		switch v.Field(i).Kind() {
		case reflect.Int:
			v.Field(i).SetInt(int64(i + 1))
		case reflect.Float64:
			v.Field(i).SetFloat(float64(i) + 1.5)
		}
	}

	got := reflect.ValueOf(rowValue.counters())
	typ := got.Type()
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if name == "FlaggedAuthors" {
			continue
		}
		if got.Field(i).IsZero() {
			t.Errorf("Counters.%s was not carried from CountersRow", name)
		}
	}
}
