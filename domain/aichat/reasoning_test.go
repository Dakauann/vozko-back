package aichat

import "testing"

func TestChooseModelCarriesTheLevelWithTheModel(t *testing.T) {
	cases := []struct {
		name               string
		model, level       string
		wantModel, wantLvl string
	}{
		{name: "a new model brings its level", model: " lab/fast ", level: " low ", wantModel: "lab/fast", wantLvl: "low"},
		{name: "a new model without a level uses its default", model: "lab/fast", level: "", wantModel: "lab/fast", wantLvl: ""},
		{name: "a level alone applies to the current model", model: "", level: "medium", wantModel: "lab/thinker", wantLvl: "medium"},
		{name: "nothing chosen keeps both", model: "", level: "", wantModel: "lab/thinker", wantLvl: "high"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			thread := &Thread{Model: "lab/thinker", ReasoningLevel: "high"}
			thread.ChooseModel(tc.model, tc.level)
			if thread.Model != tc.wantModel || thread.ReasoningLevel != tc.wantLvl {
				t.Fatalf("thread = %q / %q, want %q / %q", thread.Model, thread.ReasoningLevel, tc.wantModel, tc.wantLvl)
			}
		})
	}
}

func TestReasoningLevelForKeepsTheLevelOnlyForTheSameModel(t *testing.T) {
	thread := &Thread{Model: "lab/thinker", ReasoningLevel: "high"}
	if got := thread.ReasoningLevelFor("lab/thinker"); got != "high" {
		t.Fatalf("same model level = %q, want high", got)
	}
	if got := thread.ReasoningLevelFor("lab/other"); got != "" {
		t.Fatalf("another model level = %q, want the model default", got)
	}
}
