package audience

import (
	"reflect"
	"strings"
	"testing"

	"vozko/domain/shared"
)

// Each enum accepts exactly its own values and nothing adjacent. Values arrive
// from a model's JSON and from URL query strings, neither of which is
// normalised on the way in, so a near-miss must be rejected rather than
// silently stored.
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

// The rubric and the enums must not drift. This is the guard the legacy engine
// did not have: its taxonomy was restated in the domain, the tool schema and
// two prompt strings, and they diverged. Here every option the model is offered
// has to be a value the domain will accept back, or the model is being invited
// to answer something we then reject as invalid.
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

// Every enum value must be OFFERED, not just accepted. A disposition the domain
// stores but the rubric never describes can only ever be reached by accident.
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

// The weights must sum to 1, or the score cannot span its own range: a perfect
// conversation would top out below 100 and the number would be quietly wrong
// rather than visibly broken.
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

	// Weighted, not averaged: the heaviest dimension alone must outscore the
	// lightest alone, otherwise the weights are not being applied.
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

// A partially rated conversation is a failed classification, not a score
// computed from zero values. Without this, a model that omits one dimension
// silently produces a lower number that looks like a real assessment.
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

// The map constructor is what the decoder uses, so it must cover every
// dimension the rubric declares; a key added to one and not the other would
// silently rate as none.
func TestNewConversationQualityCoversEveryDimension(t *testing.T) {
	levels := map[string]shared.QualityLevel{}
	for _, d := range ConversationQualityDimensions() {
		levels[d.Key] = shared.QualityLevelHigh
	}
	if got := NewConversationQuality(levels); !got.Valid() || got.Score() != 100 {
		t.Errorf("constructed from every dimension key, got %+v score %d", got, got.Score())
	}
}

// The prompts must actually name the values and dimensions, since that text is
// the model's only contract.
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

// The comment taxonomy and the conversation taxonomy are different subjects and
// must not be merged by accident: a conversation has no stance and a comment has
// no disposition.
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
	// Sentiment is the one they legitimately share.
	if !commentKeys[FieldSentiment] {
		t.Error("comment rubric lost sentiment")
	}
}

func TestConversationValueListsMirrorValid(t *testing.T) {
	// Guards against a constant added to the type but not to its Values list,
	// which would make it storable but never offered or filterable.
	if got := len(InterestValues()); got != 3 {
		t.Errorf("InterestValues has %d entries", got)
	}
	if !reflect.DeepEqual(DispositionValues(), []string{"sale", "filling_info", "callback", "declined", "pending"}) {
		t.Errorf("DispositionValues = %v", DispositionValues())
	}
	// no_answer and voicemail were voice-call outcomes ("apenas para chamadas de
	// voz" in the rubric) on a product with no voice channel. Offering them gave
	// the model an escape hatch that produced a meaningless label on a messaging
	// conversation, which is exactly what it did.
	for _, gone := range []Disposition{"no_answer", "voicemail"} {
		if gone.Valid() {
			t.Errorf("%q is a voice-call outcome and must not be a valid disposition", gone)
		}
	}
	if !reflect.DeepEqual(QualificationValues(), []string{"hot_lead", "warm_lead", "cold_lead"}) {
		t.Errorf("QualificationValues = %v", QualificationValues())
	}
}
