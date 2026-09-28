package copilottools

import (
	"fmt"
	"strings"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/copilot"
)

func TestTemplateSendFailure_MonthlySendCapPointsToAdministration(t *testing.T) {
	result := templateSendFailure(fmt.Errorf("charge: %w", balance.ErrMonthlySendCapReached))

	if result.Status != copilot.StatusError {
		t.Fatalf("status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "limite mensal") || !strings.Contains(result.Message, "administração") {
		t.Fatalf("message %q must name the monthly limit and the administration", result.Message)
	}
}
