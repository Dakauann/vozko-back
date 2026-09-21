package conversation_usecase

import (
	"strings"
	"testing"

	ca "vozko/domain/audience"
)

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

		for _, d := range ca.ConversationQualityDimensions() {
			if !strings.Contains(prompt, d.Key) {
				t.Errorf("%s prompt is missing rubric dimension %q", at, d.Key)
			}
		}
		for _, f := range ca.ConversationClassificationFields() {
			if !strings.Contains(prompt, f.Key) {
				t.Errorf("%s prompt is missing classification field %q", at, f.Key)
			}
		}

		if !strings.Contains(prompt, "OBJETIVO") {
			t.Errorf("%s prompt is missing the objective framing", at)
		}
		for _, banned := range []string{"CPF", "peso 35%"} {
			if strings.Contains(prompt, banned) {
				t.Errorf("%s prompt still contains sales-biased/divergent literal %q", at, banned)
			}
		}
	}
}
