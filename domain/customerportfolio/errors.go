package customerportfolio

import "errors"

var (
	ErrWorkspaceRequired = errors.New("customerportfolio: workspace is required")
	ErrHolderRequired    = errors.New("customerportfolio: the carteira holder is required")
	ErrSubjectInvalid    = errors.New("customerportfolio: a subject is one lead or one channel contact")
	ErrSubjectNotFound   = errors.New("customerportfolio: the lead or contact was not found in this workspace")
	ErrEntryNotFound     = errors.New("customerportfolio: carteira entry not found")
	ErrForbidden         = errors.New("customerportfolio: not allowed")
	ErrHolderIneligible  = errors.New("customerportfolio: the member cannot hold a carteira")
	ErrHolderOutOfReach  = errors.New("customerportfolio: the member is outside the people you can see")
	ErrHeldByOther       = errors.New("customerportfolio: the customer is in another member's carteira")
	ErrListQueryInvalid  = errors.New("customerportfolio: the list asks for a negative page or a search that is too long")
)

type HeldByOther struct {
	Entry      Entry
	HolderName string
}

func (e *HeldByOther) Error() string { return ErrHeldByOther.Error() }

func (e *HeldByOther) Unwrap() error { return ErrHeldByOther }

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrForbidden, "forbidden"},
	{ErrHolderOutOfReach, "customer_portfolio_holder_out_of_reach"},
	{ErrHolderIneligible, "customer_portfolio_holder_ineligible"},
	{ErrHeldByOther, "customer_portfolio_held_by_other"},
	{ErrEntryNotFound, "customer_portfolio_entry_not_found"},
	{ErrSubjectNotFound, "customer_portfolio_subject_not_found"},
	{ErrSubjectInvalid, "customer_portfolio_subject_invalid"},
	{ErrHolderRequired, "customer_portfolio_holder_required"},
	{ErrListQueryInvalid, "customer_portfolio_list_invalid"},
	{ErrWorkspaceRequired, "customer_portfolio_workspace_required"},
}

var inputRefusals = []error{ErrSubjectInvalid, ErrHolderRequired, ErrListQueryInvalid, ErrWorkspaceRequired}

func ErrorCode(err error) string {
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}

func IsInputRefusal(err error) bool {
	for _, refusal := range inputRefusals {
		if errors.Is(err, refusal) {
			return true
		}
	}
	return false
}
