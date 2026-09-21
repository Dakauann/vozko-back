package audience

import (
	"reflect"
	"strings"
	"testing"

	"vozko/domain/shared"
)

func TestConversationEnumsRejectNearMisses(t *testing.T) {
	for _, v := range InterestValues() {
		if !Interest(v).Valid() {
			t.Errorf("Interest(%q) should be valid", v)
		}
	}
	for _, v := range []string{"", "Interested", "INTERESTED", "interested ", "maybe"} {
		if Interest(v).Valid() {
			t.Errorf("Interest(%q) should be invalid", v)
		}
	}

	for _, v := range DispositionValues() {
		if !Disposition(v).Valid() {
			t.Errorf("Disposition(%q) should be valid", v)
		}
	}
	for _, v := range []string{"", "Sale", "sold", "filling info", "pending\n"} {
		if Disposition(v).Valid() {
			t.Errorf("Disposition(%q) should be invalid", v)
		}
	}

	for _, v := range QualificationValues() {
		if !Qualification(v).Valid() {
			t.Errorf("Qualification(%q) should be valid", v)
		}
	}
	for _, v := range []string{"", "hot", "HOT_LEAD", "hot lead"} {
		if Qualification(v).Valid() {
			t.Errorf("Qualification(%q) should be invalid", v)
		}
	}

	for _, v := range NextActionValues() {
		if !NextAction(v).Valid() {
			t.Errorf("NextAction(%q) should be valid", v)
		}
	}
	for _, v := range []string{"", "Close", "call_back", "continue "} {
		if NextAction(v).Valid() {
			t.Errorf("NextAction(%q) should be invalid", v)
		}
	}
}

func TestConversationRubricOptionsAreValidEnumValues(t *testing.T) {
	valid := map[string]func(string) bool{
		FieldInterest:      func(v string) bool { return Interest(v).Valid() },
		FieldDisposition:   func(v string) bool { return Disposition(v).Valid() },
		FieldSentiment:     func(v string) bool { return shared.Sentiment(v).Valid() },
		FieldQualification: func(v string) bool { return Qualification(v).Valid() },
		FieldNextAction:    func(v string) bool { return NextAction(v).Valid() },
	}

	fields := ConversationClassificationFields()
	if len(fields) != len(valid) {
		t.Fatalf("rubric has %d fields, expected %d", len(fields), len(valid))
	}

	seen := map[string]bool{}
	for _, f := range fields {
		check, ok := valid[f.Key]
		if !ok {
			t.Errorf("rubric field %q is not one of the known field keys", f.Key)
			continue
		}
		seen[f.Key] = true
		if len(f.Options) == 0 {
			t.Errorf("field %q offers the model no options", f.Key)
		}
		for _, opt := range f.Options {
			if !check(opt.Value) {
				t.Errorf("field %q offers %q, which the domain rejects", f.Key, opt.Value)
			}
			if opt.Description == "" {
				t.Errorf("field %q option %q has no criteria, so the model is guessing", f.Key, opt.Value)
			}
		}
	}
	for key := range valid {
		if !seen[key] {
			t.Errorf("field %q is missing from the rubric", key)
		}
	}
}

func TestConversationRubricOffersEveryEnumValue(t *testing.T) {
	offered := map[string]map[string]bool{}
	for _, f := range ConversationClassificationFields() {
		offered[f.Key] = map[string]bool{}
		for _, opt := range f.Options {
			offered[f.Key][opt.Value] = true
		}
	}

	for field, values := range map[string][]string{
		FieldInterest:      InterestValues(),
		FieldDisposition:   DispositionValues(),
		FieldQualification: QualificationValues(),
		FieldNextAction:    NextActionValues(),
	} {
		for _, v := range values {
			if !offered[field][v] {
				t.Errorf("%s value %q is never offered to the model", field, v)
			}
		}
	}
}

func TestConversationQualityWeightsSumToOne(t *testing.T) {
	var sum float64
	for _, d := range ConversationQualityDimensions() {
		if d.Weight <= 0 {
			t.Errorf("dimension %q has non-positive weight %v", d.Key, d.Weight)
		}
		if d.Label == "" || d.Description == "" {
			t.Errorf("dimension %q is not described, so the model is guessing", d.Key)
		}
		sum += d.Weight
	}
	if diff := sum - 1.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("weights sum to %v, want 1.0", sum)
	}
}

func TestConversationQualityScoreSpansItsRange(t *testing.T) {
	all := func(l shared.QualityLevel) ConversationQuality {
		return ConversationQuality{GoalProgress: l, CustomerEngagement: l, AgentConduct: l, Professionalism: l}
	}
	if got := all(shared.QualityLevelNone).Score(); got != 0 {
		t.Errorf("all none scored %d, want 0", got)
	}
	if got := all(shared.QualityLevelHigh).Score(); got != 100 {
		t.Errorf("all high scored %d, want 100", got)
	}

	goal := ConversationQuality{GoalProgress: shared.QualityLevelHigh}
	prof := ConversationQuality{Professionalism: shared.QualityLevelHigh}
	if goal.Score() <= prof.Score() {
		t.Errorf("goal_progress (%d) should outweigh professionalism (%d)", goal.Score(), prof.Score())
	}

	mixed := ConversationQuality{
		GoalProgress:       shared.QualityLevelMedium,
		CustomerEngagement: shared.QualityLevelLow,
		AgentConduct:       shared.QualityLevelHigh,
		Professionalism:    shared.QualityLevelNone,
	}
	if s := mixed.Score(); s < 0 || s > 100 {
		t.Errorf("score %d out of range", s)
	}
}

func TestConversationQualityValidRequiresEveryDimension(t *testing.T) {
	full := ConversationQuality{
		GoalProgress:       shared.QualityLevelHigh,
		CustomerEngagement: shared.QualityLevelMedium,
		AgentConduct:       shared.QualityLevelLow,
		Professionalism:    shared.QualityLevelNone,
	}
	if !full.Valid() {
		t.Error("a fully rated assessment should be valid")
	}

	partial := full
	partial.AgentConduct = ""
	if partial.Valid() {
		t.Error("an assessment missing a dimension must not be valid")
	}

	bogus := full
	bogus.GoalProgress = "excellent"
	if bogus.Valid() {
		t.Error("an assessment with an unknown level must not be valid")
	}
}

func TestNewConversationQualityCoversEveryDimension(t *testing.T) {
	levels := map[string]shared.QualityLevel{}
	for _, d := range ConversationQualityDimensions() {
		levels[d.Key] = shared.QualityLevelHigh
	}
	if got := NewConversationQuality(levels); !got.Valid() || got.Score() != 100 {
		t.Errorf("constructed from every dimension key, got %+v score %d", got, got.Score())
	}
}

func TestConversationPromptsRenderTheRubric(t *testing.T) {
	prompt := ConversationRubricPrompt()
	for _, v := range append(append(InterestValues(), DispositionValues()...), NextActionValues()...) {
		if !strings.Contains(prompt, v) {
			t.Errorf("classification prompt never mentions %q", v)
		}
	}

	quality := ConversationQualityRubricPrompt()
	for _, d := range ConversationQualityDimensions() {
		if !strings.Contains(quality, d.Key) {
			t.Errorf("quality prompt never mentions dimension %q", d.Key)
		}
	}
	for _, lvl := range shared.QualityLevelValues() {
		if !strings.Contains(quality, lvl) {
			t.Errorf("quality prompt never mentions level %q", lvl)
		}
	}
}

func TestCommentAndConversationTaxonomiesStayDistinct(t *testing.T) {
	commentKeys := map[string]bool{}
	for _, f := range ClassificationFields() {
		commentKeys[f.Key] = true
	}
	for _, f := range ConversationClassificationFields() {
		if f.Key == FieldStance || f.Key == FieldIntent {
			t.Errorf("conversation rubric must not carry the comment field %q", f.Key)
		}
	}
	for _, key := range []string{FieldInterest, FieldDisposition, FieldQualification, FieldNextAction} {
		if commentKeys[key] {
			t.Errorf("comment rubric must not carry the conversation field %q", key)
		}
	}
	if !commentKeys[FieldSentiment] {
		t.Error("comment rubric lost sentiment")
	}
}

func TestConversationValueListsMirrorValid(t *testing.T) {
	if got := len(InterestValues()); got != 3 {
		t.Errorf("InterestValues has %d entries", got)
	}
	if !reflect.DeepEqual(DispositionValues(), []string{"sale", "filling_info", "callback", "declined", "pending"}) {
		t.Errorf("DispositionValues = %v", DispositionValues())
	}
	for _, gone := range []Disposition{"no_answer", "voicemail"} {
		if gone.Valid() {
			t.Errorf("%q is a voice-call outcome and must not be a valid disposition", gone)
		}
	}
	if !reflect.DeepEqual(QualificationValues(), []string{"hot_lead", "warm_lead", "cold_lead"}) {
		t.Errorf("QualificationValues = %v", QualificationValues())
	}
}
