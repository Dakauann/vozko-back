package opportunity_usecase

import (
	"errors"
	"fmt"
	"testing"

	"vozko/domain/customfield"
	"vozko/domain/opportunity"
)

func TestRefusalOfNamesEveryBusinessRule(t *testing.T) {
	cases := map[error]Refusal{
		opportunity.ErrWonWithoutValue:      RefusalWonWithoutValue,
		ErrNoOpenDeal:                       RefusalNoOpenDeal,
		opportunity.ErrLostReasonMissing:    RefusalLostReasonMissing,
		opportunity.ErrTitleOrLead:          RefusalTitleMissing,
		opportunity.ErrStageRequired:        RefusalStageInvalid,
		opportunity.ErrStageOutsidePipeline: RefusalStageInvalid,
		ErrStageNotFound:                    RefusalStageInvalid,
		opportunity.ErrInvalidAmount:        RefusalAmountInvalid,
		opportunity.ErrNegativeValue:        RefusalAmountInvalid,
		opportunity.ErrUnsupportedCurrency:  RefusalCurrencyUnsupported,
		ErrPipelineNotFound:                 RefusalPipelineInvalid,
		ErrNotOpportunityPipeline:           RefusalPipelineInvalid,
		opportunity.ErrPipelineRequired:     RefusalPipelineInvalid,
		ErrPipelineHasNoOpenStage:           RefusalPipelineInvalid,
		ErrPipelineHasNoWonStage:            RefusalPipelineInvalid,
		ErrPipelineHasNoLostStage:           RefusalPipelineInvalid,
		customfield.ErrValueRequired:        RefusalRequiredFields,
	}
	for err, want := range cases {
		got, ok := RefusalOf(fmt.Errorf("wrapped: %w", err))
		if !ok || got != want {
			t.Fatalf("RefusalOf(%v) = %q, %v, want %q", err, got, ok, want)
		}
	}
	if _, ok := RefusalOf(errors.New("connection reset")); ok {
		t.Fatalf("an infrastructure failure was classified as a business refusal")
	}
}

func TestRefusalsListsEachRefusalOnce(t *testing.T) {
	seen := map[Refusal]bool{}
	for _, r := range Refusals() {
		if seen[r] {
			t.Fatalf("refusal %q listed twice", r)
		}
		seen[r] = true
	}
	if len(seen) != 9 {
		t.Fatalf("Refusals() = %d refusals, want 9", len(seen))
	}
}
