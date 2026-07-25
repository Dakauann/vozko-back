package analysis

import "testing"

func TestQualityAssessment_Score(t *testing.T) {
	cases := []struct {
		name string
		a    QualityAssessment
		want int
	}{
		{"all high = 100", QualityAssessment{QualityLevelHigh, QualityLevelHigh, QualityLevelHigh, QualityLevelHigh}, 100},
		{"all none = 0", QualityAssessment{QualityLevelNone, QualityLevelNone, QualityLevelNone, QualityLevelNone}, 0},
		{"all medium ~= 66", QualityAssessment{QualityLevelMedium, QualityLevelMedium, QualityLevelMedium, QualityLevelMedium}, 66},
		{"goal high only = 40", QualityAssessment{QualityLevelHigh, QualityLevelNone, QualityLevelNone, QualityLevelNone}, 40},
		{"engagement high only = 30", QualityAssessment{QualityLevelNone, QualityLevelHigh, QualityLevelNone, QualityLevelNone}, 30},
		// 0.40*1 + 0.30*0.33 + 0.20*0.66 + 0.10*0 = 0.631 -> 63
		{"mixed = 63", QualityAssessment{QualityLevelHigh, QualityLevelLow, QualityLevelMedium, QualityLevelNone}, 63},
	}
	for _, tc := range cases {
		if got := tc.a.Score(); got != tc.want {
			t.Errorf("%s: Score() = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestQualityAssessment_ScoreAlwaysInRange(t *testing.T) {
	levels := []QualityLevel{QualityLevelNone, QualityLevelLow, QualityLevelMedium, QualityLevelHigh, QualityLevel("garbage")}
	for _, a := range levels {
		for _, b := range levels {
			qa := QualityAssessment{a, b, a, b}
			if s := qa.Score(); s < 0 || s > 100 {
				t.Fatalf("Score() = %d out of [0,100] for %v", s, qa)
			}
		}
	}
}

func TestNewQualityAssessment_MapsKeys(t *testing.T) {
	a := NewQualityAssessment(map[string]QualityLevel{
		QualityKeyGoalProgress:       QualityLevelHigh,
		QualityKeyCustomerEngagement: QualityLevelLow,
		QualityKeyAgentConduct:       QualityLevelMedium,
		QualityKeyProfessionalism:    QualityLevelNone,
	})
	if a.GoalProgress != QualityLevelHigh || a.CustomerEngagement != QualityLevelLow ||
		a.AgentConduct != QualityLevelMedium || a.Professionalism != QualityLevelNone {
		t.Fatalf("NewQualityAssessment mapped keys incorrectly: %+v", a)
	}
}

func TestQualityDimensions_WeightsSumToOne(t *testing.T) {
	var sum float64
	for _, d := range QualityDimensions() {
		sum += d.Weight
	}
	if sum < 0.999 || sum > 1.001 {
		t.Fatalf("dimension weights sum to %v, want 1.0", sum)
	}
}

func TestClassificationFields_ValuesMatchDomainEnums(t *testing.T) {
	valid := map[string]func(string) bool{
		"interest":      func(v string) bool { return Interest(v).Valid() },
		"disposition":   func(v string) bool { return Disposition(v).Valid() },
		"sentiment":     func(v string) bool { return Sentiment(v).Valid() },
		"qualification": func(v string) bool { return Qualification(v).Valid() },
		"next_action":   func(v string) bool { return NextAction(v).Valid() },
	}
	for _, f := range ClassificationFields() {
		check, ok := valid[f.Key]
		if !ok {
			t.Fatalf("unknown classification field key %q", f.Key)
		}
		if len(f.Options) == 0 {
			t.Errorf("field %q has no options", f.Key)
		}
		for _, v := range f.Values() {
			if !check(v) {
				t.Errorf("field %q value %q is not valid per its domain enum", f.Key, v)
			}
		}
	}
}

func TestQualityLevel_Valid(t *testing.T) {
	for _, v := range QualityLevelValues() {
		if !QualityLevel(v).Valid() {
			t.Errorf("%q should be valid", v)
		}
	}
	if QualityLevel("bogus").Valid() {
		t.Error("bogus should be invalid")
	}
}
