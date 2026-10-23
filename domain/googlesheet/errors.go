package googlesheet

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidSheet           = errors.New("googlesheet: invalid sheet definition")
	ErrSheetNotFound          = errors.New("googlesheet: sheet not found")
	ErrSheetNotActive         = errors.New("googlesheet: the sheet needs attention before it accepts rows")
	ErrStaleSheet             = errors.New("googlesheet: the sheet changed since it was read")
	ErrUnknownColumn          = errors.New("googlesheet: the sheet has no such column")
	ErrColumnArchived         = errors.New("googlesheet: the column is archived")
	ErrSystemColumn           = errors.New("googlesheet: the column is filled by the system")
	ErrRequiredColumnMissing  = errors.New("googlesheet: a required column has no value")
	ErrInvalidValue           = errors.New("googlesheet: the value does not fit the column type")
	ErrValueNotInOptions      = errors.New("googlesheet: the value is not one of the column options")
	ErrValueTooLong           = errors.New("googlesheet: the value is longer than a cell holds")
	ErrRowTooLarge            = errors.New("googlesheet: the row is larger than one request holds")
	ErrUnresolvedVariable     = errors.New("googlesheet: a variable in the mapping has no value")
	ErrIdempotencyKeyRequired = errors.New("googlesheet: an idempotency key is required")
	ErrOriginRequired         = errors.New("googlesheet: the origin of the row is required")
	ErrAccountChanged         = errors.New("googlesheet: the sheet was created by a google account that is no longer connected for sheets")
	ErrRowNotFound            = errors.New("googlesheet: row not found")
	ErrGoogleBusy             = errors.New("googlesheet: the google quota for this minute is used up, try again in a minute")
)

const (
	CodeInvalidSheet           = "google_sheet_invalid_definition"
	CodeSheetNotFound          = "google_sheet_not_found"
	CodeSheetNotActive         = "google_sheet_not_active"
	CodeStaleSheet             = "google_sheet_changed"
	CodeUnknownColumn          = "google_sheet_unknown_column"
	CodeColumnArchived         = "google_sheet_column_archived"
	CodeSystemColumn           = "google_sheet_system_column"
	CodeRequiredColumnMissing  = "google_sheet_required_column_missing"
	CodeInvalidValue           = "google_sheet_invalid_value"
	CodeValueNotInOptions      = "google_sheet_value_not_in_options"
	CodeValueTooLong           = "google_sheet_value_too_long"
	CodeRowTooLarge            = "google_sheet_row_too_large"
	CodeUnresolvedVariable     = "google_sheet_unresolved_variable"
	CodeIdempotencyKeyRequired = "google_sheet_idempotency_key_required"
	CodeOriginRequired         = "google_sheet_origin_required"
	CodeAccountChanged         = "google_sheet_account_changed"
	CodeRowNotFound            = "google_sheet_row_not_found"
	CodeGoogleBusy             = "google_sheet_google_busy"
	CodeInternal               = "google_sheet_failed"
)

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrInvalidSheet, CodeInvalidSheet},
	{ErrSheetNotFound, CodeSheetNotFound},
	{ErrSheetNotActive, CodeSheetNotActive},
	{ErrStaleSheet, CodeStaleSheet},
	{ErrUnknownColumn, CodeUnknownColumn},
	{ErrColumnArchived, CodeColumnArchived},
	{ErrSystemColumn, CodeSystemColumn},
	{ErrRequiredColumnMissing, CodeRequiredColumnMissing},
	{ErrInvalidValue, CodeInvalidValue},
	{ErrValueNotInOptions, CodeValueNotInOptions},
	{ErrValueTooLong, CodeValueTooLong},
	{ErrRowTooLarge, CodeRowTooLarge},
	{ErrUnresolvedVariable, CodeUnresolvedVariable},
	{ErrIdempotencyKeyRequired, CodeIdempotencyKeyRequired},
	{ErrOriginRequired, CodeOriginRequired},
	{ErrAccountChanged, CodeAccountChanged},
	{ErrRowNotFound, CodeRowNotFound},
	{ErrGoogleBusy, CodeGoogleBusy},
}

func AllErrors() []error {
	out := make([]error, len(errorCodes))
	for i, entry := range errorCodes {
		out[i] = entry.err
	}
	return out
}

func Codes() []string {
	out := make([]string, len(errorCodes))
	for i, entry := range errorCodes {
		out[i] = entry.code
	}
	return out
}

func ErrorCode(err error) string {
	for _, entry := range errorCodes {
		if errors.Is(err, entry.err) {
			return entry.code
		}
	}
	return CodeInternal
}

type ValueError struct {
	Column string
	Header string
	Reason error
	Detail string
}

func (e *ValueError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("coluna %q (%s): %v", e.Header, e.Column, e.Reason)
	}
	return fmt.Sprintf("coluna %q (%s): %v: %s", e.Header, e.Column, e.Reason, e.Detail)
}

func (e *ValueError) Unwrap() error { return e.Reason }

func invalidSheet(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidSheet}, args...)...)
}
