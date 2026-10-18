package customerportfolio

import (
	"errors"
	"fmt"
	"testing"
)

func TestEveryRefusalHasAStableCode(t *testing.T) {
	cases := map[error]string{
		ErrForbidden:         "forbidden",
		ErrHolderOutOfReach:  "customer_portfolio_holder_out_of_reach",
		ErrHolderIneligible:  "customer_portfolio_holder_ineligible",
		ErrEntryNotFound:     "customer_portfolio_entry_not_found",
		ErrSubjectNotFound:   "customer_portfolio_subject_not_found",
		ErrSubjectInvalid:    "customer_portfolio_subject_invalid",
		ErrHolderRequired:    "customer_portfolio_holder_required",
		ErrListQueryInvalid:  "customer_portfolio_list_invalid",
		ErrWorkspaceRequired: "customer_portfolio_workspace_required",
		&HeldByOther{}:       "customer_portfolio_held_by_other",
		fmt.Errorf("wrapped: %w", ErrEntryNotFound): "customer_portfolio_entry_not_found",
		errors.New("db down"):                       "",
	}
	for err, want := range cases {
		if got := ErrorCode(err); got != want {
			t.Errorf("ErrorCode(%v) = %q, want %q", err, got, want)
		}
	}
}

func TestInputRefusalsAreTheCallersMistakes(t *testing.T) {
	for _, err := range []error{ErrSubjectInvalid, ErrHolderRequired, ErrListQueryInvalid, ErrWorkspaceRequired} {
		if !IsInputRefusal(err) {
			t.Errorf("%v must be an input refusal", err)
		}
	}
	for _, err := range []error{ErrForbidden, ErrHeldByOther, ErrHolderIneligible, ErrEntryNotFound} {
		if IsInputRefusal(err) {
			t.Errorf("%v is not the caller's input mistake", err)
		}
	}
}
