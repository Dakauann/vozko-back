package copilottools

import (
	"fmt"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/opportunity"
)

func TestADealOnAnotherLeadThanItsConversationIsExplained(t *testing.T) {
	result := dealFailure("create_deal", fmt.Errorf("create: %w", opportunity.ErrEntryLeadMismatch))
	if result.Status != copilot.StatusError || result.Message != dealLeadMismatch {
		t.Fatalf("result = %+v", result)
	}
}
