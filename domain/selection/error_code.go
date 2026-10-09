package selection

import (
	"errors"

	"vozko/domain/crmfilter"
)

const CodeInvalidFilter = "invalid_filter"

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrUnknownMode, "selection_unknown_mode"},
	{ErrEmptySelection, "selection_empty"},
	{ErrAmbiguousSelection, "selection_ambiguous"},
	{ErrTooManyIDs, "selection_too_many_ids"},
	{ErrFilterRequired, "selection_filter_required"},
	{ErrLimitRequired, "selection_limit_required"},
	{ErrEveryoneUnconfirmed, "selection_everyone_unconfirmed"},
	{ErrFingerprintMismatch, "selection_fingerprint_mismatch"},
	{ErrInvalidCount, "selection_invalid_count"},
	{ErrCountChanged, "selection_changed"},
	{ErrModeUnsupported, "selection_mode_unsupported"},
	{ErrResolverUnavailable, "selection_unavailable"},
	{ErrInvalidPage, "selection_invalid_page"},
	{ErrScopeDenied, "selection_scope_denied"},
	{ErrInvalidIDs, "selection_invalid_ids"},
	{ErrUnknownSort, "selection_unknown_sort"},
	{ErrCountRequired, "selection_count_required"},
}

var filterErrors = []error{
	crmfilter.ErrUnknownField,
	crmfilter.ErrUnsupportedOp,
	crmfilter.ErrMissingValue,
	crmfilter.ErrBetweenValues,
	crmfilter.ErrInvalidNumber,
	crmfilter.ErrInvalidDate,
	crmfilter.ErrMissingCustomKey,
	crmfilter.ErrNotApplicable,
	crmfilter.ErrInvalidValue,
	crmfilter.ErrTooManyValues,
	crmfilter.ErrConjunctionRequired,
}

func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	for _, invalid := range filterErrors {
		if errors.Is(err, invalid) {
			return CodeInvalidFilter
		}
	}
	return ""
}
