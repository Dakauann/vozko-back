package audience_repository

import (
	"reflect"
	"testing"
	"time"

	gormschema "gorm.io/gorm/schema"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

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

func fullyPopulatedAnalysis() *ca.Analysis {
	now := time.Date(2026, 4, 5, 10, 30, 0, 0, time.UTC)
	later := now.Add(time.Hour)
	parent := "parent-comment-1"

	return &ca.Analysis{
		ID:               "11111111-1111-1111-1111-111111111111",
		WorkspaceID:      "22222222-2222-2222-2222-222222222222",
		SubjectKind:      ca.SubjectKindConversation,
		Revision:         "9f2c1ab3d4e5",
		Transcript:       "Cliente: ola. Atendente: bom dia",
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

		Interest:           ca.InterestInterested,
		ProductInterest:    "plano familia",
		ProductInterestKey: "plano familia",
		Disposition:        ca.DispositionFillingInfo,
		Qualification:      ca.QualificationHotLead,
		NextAction:         ca.NextActionEscalate,
		Summary:            "Cliente pediu orcamento e enviou documentos.",
		AttendanceQuality:  88,
		MessageCount:       17,

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

func TestCountersRowCoversEveryDomainCounter(t *testing.T) {
	row := reflect.TypeOf(CountersRow{})
	inRow := map[string]bool{}
	for i := 0; i < row.NumField(); i++ {
		inRow[row.Field(i).Name] = true
	}

	counters := reflect.TypeOf(ca.Counters{})
	for i := 0; i < counters.NumField(); i++ {
		name := counters.Field(i).Name
		if name == "FlaggedAuthors" {
			continue
		}
		if !inRow[name] {
			t.Errorf("Counters.%s has no column in CountersRow, so it is always zero", name)
		}
	}
}

func TestCountersMappingCarriesEveryField(t *testing.T) {
	var rowValue CountersRow
	v := reflect.ValueOf(&rowValue).Elem()
	for i := 0; i < v.NumField(); i++ {
		switch v.Field(i).Kind() {
		case reflect.Int:
			v.Field(i).SetInt(int64(i + 1))
		case reflect.Float64:
			v.Field(i).SetFloat(float64(i) + 1.5)
		case reflect.Ptr:
			v.Field(i).Set(reflect.New(v.Field(i).Type().Elem()))
			if ts, ok := v.Field(i).Interface().(*time.Time); ok {
				*ts = time.Date(2026, 4, 5, 10, 30, 0, 0, time.UTC)
			}
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

func TestSaveColumnsCoverEveryMutableField(t *testing.T) {
	immutable := map[string]bool{
		"id": true, "workspace_id": true, "subject_kind": true, "revision": true,
		"transcript": true, "source": true, "account_id": true, "container_id": true,
		"subject_id": true, "parent_subject_id": true, "author_external_id": true,
		"author_handle": true, "excerpt": true, "created_at": true,
	}

	written := saveColumns(fullyPopulatedAnalysis())
	typ := reflect.TypeOf(schema.AudienceAnalysis{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		column := naming.ColumnName("", field.Name)
		if immutable[column] {
			continue
		}
		if _, ok := written[column]; !ok {
			t.Errorf("saveColumns never writes %s, so it keeps whatever the insert put there forever", column)
		}
	}
}

var naming = gormschema.NamingStrategy{}
