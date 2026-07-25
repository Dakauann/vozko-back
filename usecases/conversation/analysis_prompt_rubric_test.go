package conversation_usecase

import (
	"strings"
	"testing"

	"vozko/domain/analysis"
)

// Guards the rubric single-sourcing: both prompts must render the domain quality
// dimensions and must not contain a Go format-error marker (which would mean the
// Sprintf argument count drifted from the format string).
func TestBuildAnalysisPrompt_RubricRenderedNoFormatError(t *testing.T) {
	for _, at := range []AnalysisType{AnalysisTypeOngoing, AnalysisTypeCompleted} {
		prompt := BuildAnalysisPrompt(AnalysisPromptInput{
			AnalysisType:    at,
			CampaignName:    "Campanha Teste",
			UserPhoneNumber: "+5511999999999",
			MessageCount:    3,
		})

		if strings.Contains(prompt, "%!") {
			t.Fatalf("%s prompt contains a format-error marker (Sprintf arg mismatch):\n%s", at, prompt)
		}

		// Quality dimensions render (single-sourced).
		for _, d := range analysis.QualityDimensions() {
			if !strings.Contains(prompt, d.Key) {
				t.Errorf("%s prompt is missing rubric dimension %q", at, d.Key)
			}
		}
		// Classification fields render (single-sourced).
		for _, f := range analysis.ClassificationFields() {
			if !strings.Contains(prompt, f.Key) {
				t.Errorf("%s prompt is missing classification field %q", at, f.Key)
			}
		}

		// Niche-neutral framing must be present...
		if !strings.Contains(prompt, "OBJETIVO") {
			t.Errorf("%s prompt is missing the objective framing", at)
		}
		// ...and the sales-specific literals that biased non-sales niches must be gone.
		for _, banned := range []string{"CPF", "peso 35%"} {
			if strings.Contains(prompt, banned) {
				t.Errorf("%s prompt still contains sales-biased/divergent literal %q", at, banned)
			}
		}
	}
}
