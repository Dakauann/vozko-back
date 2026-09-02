package template

import (
	"errors"
	"sort"
)

// Error codes are the stable, machine-readable identity of a template failure.
//
// They exist because the message is not a UI string. Every sentinel in this
// package spells its reason in English, written for whoever is reading a log,
// and the product is used in Portuguese — so a template rejected for a body
// variable in the wrong place told a Brazilian operator "body text cannot start
// with a variable - add text before it". The client cannot translate that: the
// prose is not a key, and matching on it would break the first time somebody
// improves the wording.
//
// A code is a key. The UI looks it up, renders the sentence in the operator's
// language, and falls back to the server's message when it meets a code it does
// not know yet — so a new backend error degrades to English rather than to
// nothing.
//
// The values are part of the API contract. Renaming one is a breaking change in
// the same way renaming a JSON field is; add a new code instead.
const (
	CodeUnknown = "template_unknown_error"

	CodeNotFound              = "template_not_found"
	CodeAlreadyExists         = "template_already_exists"
	CodeExternalIDRequired    = "template_external_id_required"
	CodeCategoryUnavailable   = "template_category_unavailable"
	CodeHeaderMediaNotAllowed = "template_header_media_not_applicable"

	CodeNameRequired        = "template_name_required"
	CodeNameInvalidChars    = "template_name_invalid_chars"
	CodeNameMustStartLetter = "template_name_must_start_letter"
	CodeNameTooLong         = "template_name_too_long"

	CodeHeaderTextTooLong          = "template_header_text_too_long"
	CodeHeaderTextTooManyVariables = "template_header_too_many_variables"
	CodeHeaderFormatRequired       = "template_header_format_required"
	CodeHeaderMediaNeedsHandle     = "template_header_media_needs_handle"

	CodeBodyTextTooLong          = "template_body_too_long"
	CodeBodyVariableAtStart      = "template_body_variable_at_start"
	CodeBodyVariableAtEnd        = "template_body_variable_at_end"
	CodeBodyConsecutiveVariables = "template_body_consecutive_variables"
	CodeBodyNeedsExample         = "template_body_needs_example"

	CodeFooterTextTooLong  = "template_footer_too_long"
	CodeFooterHasVariables = "template_footer_has_variables"

	CodeTooManyButtons          = "template_too_many_buttons"
	CodeButtonTextTooLong       = "template_button_text_too_long"
	CodeButtonTextRequired      = "template_button_text_required"
	CodeButtonURLRequired       = "template_button_url_required"
	CodeButtonPhoneRequired     = "template_button_phone_required"
	CodeButtonsNotGrouped       = "template_buttons_not_grouped"
	CodeURLButtonVariableNotEnd = "template_url_button_variable_not_end"
	CodeURLButtonTooManyVars    = "template_url_button_too_many_variables"
	CodeCopyCodeNeedsExample    = "template_copy_code_needs_example"

	CodeCallPermissionWithButtons      = "template_call_permission_with_buttons"
	CodeMultipleCallPermissionRequests = "template_multiple_call_permission"

	CodeMixedParameterStyles = "template_mixed_parameter_styles"

	CodeInvalidComponentType = "template_invalid_component_type"
	CodeInvalidHeaderFormat  = "template_invalid_header_format"
	CodeInvalidButtonType    = "template_invalid_button_type"
	CodeInvalidCategory      = "template_invalid_category"

	// The SEND path. Same contract, different failures: these reach the UI from
	// the send button rather than the create form, and were equally unlocalised.
	CodeSendWorkspaceRequired    = "template_send_workspace_required"
	CodeSendIdempotencyRequired  = "template_send_idempotency_required"
	CodeSendInProgress           = "template_send_in_progress"
	CodeSendPhoneMismatch        = "template_send_phone_mismatch"
	CodeSendPricingUnavailable   = "template_send_pricing_unavailable"
	CodeSendNotSendable          = "template_send_not_sendable"
	CodeSendBillingNotConfigured = "template_send_billing_not_configured"
	CodeSendAttemptConflict      = "template_send_attempt_conflict"

	// CodeProviderRejected is a rejection by WhatsApp itself rather than by our
	// validation. The accompanying message is Meta's own error_user_msg, which
	// Meta already localises — so the UI shows it verbatim instead of trying to
	// translate a sentence it did not write.
	CodeProviderRejected = "template_provider_rejected"
	// CodeProviderUnavailable is a transport or 5xx failure reaching Meta. It is
	// the one class worth retrying, and the only one that is genuinely our
	// problem rather than the operator's.
	CodeProviderUnavailable = "template_provider_unavailable"
)

// errorCodes maps every sentinel to its code. Kept as one table rather than a
// switch so the coverage test can walk it, and so adding a sentinel without a
// code is a visible omission in one place instead of a silent fallthrough.
var errorCodes = map[error]string{
	ErrTemplateNotFound:            CodeNotFound,
	ErrTemplateAlreadyExists:       CodeAlreadyExists,
	ErrExternalIDRequired:          CodeExternalIDRequired,
	ErrTemplateCategoryUnavailable: CodeCategoryUnavailable,
	ErrHeaderMediaURLNotApplicable: CodeHeaderMediaNotAllowed,

	ErrTemplateNameRequired:        CodeNameRequired,
	ErrTemplateNameInvalidChars:    CodeNameInvalidChars,
	ErrTemplateNameMustStartLetter: CodeNameMustStartLetter,
	ErrTemplateNameTooLong:         CodeNameTooLong,

	ErrHeaderTextTooLong:          CodeHeaderTextTooLong,
	ErrHeaderTextTooManyVariables: CodeHeaderTextTooManyVariables,
	ErrHeaderFormatRequired:       CodeHeaderFormatRequired,
	ErrHeaderMediaNeedsHandle:     CodeHeaderMediaNeedsHandle,

	ErrBodyTextTooLong:          CodeBodyTextTooLong,
	ErrBodyVariableAtStart:      CodeBodyVariableAtStart,
	ErrBodyVariableAtEnd:        CodeBodyVariableAtEnd,
	ErrBodyConsecutiveVariables: CodeBodyConsecutiveVariables,
	ErrBodyNeedsExample:         CodeBodyNeedsExample,

	ErrFooterTextTooLong:  CodeFooterTextTooLong,
	ErrFooterHasVariables: CodeFooterHasVariables,

	ErrTooManyButtons:          CodeTooManyButtons,
	ErrButtonTextTooLong:       CodeButtonTextTooLong,
	ErrButtonTextRequired:      CodeButtonTextRequired,
	ErrButtonURLRequired:       CodeButtonURLRequired,
	ErrButtonPhoneRequired:     CodeButtonPhoneRequired,
	ErrButtonsNotGrouped:       CodeButtonsNotGrouped,
	ErrURLButtonVariableNotEnd: CodeURLButtonVariableNotEnd,
	ErrURLButtonTooManyVars:    CodeURLButtonTooManyVars,
	ErrCopyCodeNeedsExample:    CodeCopyCodeNeedsExample,

	ErrCallPermissionWithButtons:      CodeCallPermissionWithButtons,
	ErrMultipleCallPermissionRequests: CodeMultipleCallPermissionRequests,

	ErrMixedParameterStyles: CodeMixedParameterStyles,

	ErrInvalidComponentType: CodeInvalidComponentType,
	ErrInvalidHeaderFormat:  CodeInvalidHeaderFormat,
	ErrInvalidButtonType:    CodeInvalidButtonType,
	ErrInvalidCategory:      CodeInvalidCategory,

	ErrWorkspaceRequired:      CodeSendWorkspaceRequired,
	ErrIdempotencyKeyRequired: CodeSendIdempotencyRequired,
	ErrSendInProgress:         CodeSendInProgress,
	ErrTemplatePhoneMismatch:  CodeSendPhoneMismatch,
	ErrPricingUnavailable:     CodeSendPricingUnavailable,
	ErrTemplateNotSendable:    CodeSendNotSendable,
	ErrBillingNotConfigured:   CodeSendBillingNotConfigured,
	ErrSendAttemptConflict:    CodeSendAttemptConflict,
}

// ErrorCode returns the stable code for an error, walking the wrap chain.
//
// An unrecognised error yields CodeUnknown rather than an empty string: the UI
// branches on the code, and "" would be an invisible third state alongside
// "known" and "unknown".
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	for sentinel, code := range errorCodes {
		if errors.Is(err, sentinel) {
			return code
		}
	}
	return CodeUnknown
}

// IsValidationError reports whether an error is the caller's to fix.
//
// It is derived from the code table rather than from a second hand-maintained
// list — the previous handler kept its own slice of ~25 sentinels, which is a
// list that silently falls out of date every time an error is added.
func IsValidationError(err error) bool {
	if err == nil {
		return false
	}
	for sentinel := range errorCodes {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

// KnownErrorCodes returns every code this build can emit, sorted.
//
// The UI's translation table is checked against this in a test, so a code
// without a translation is caught here rather than by an operator meeting a raw
// English sentence in production.
func KnownErrorCodes() []string {
	codes := make([]string, 0, len(errorCodes)+3)
	seen := map[string]bool{}
	for _, code := range errorCodes {
		if !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	for _, code := range []string{CodeUnknown, CodeProviderRejected, CodeProviderUnavailable} {
		if !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	return codes
}

// ProviderError is a failure that came from WhatsApp itself rather than from
// our validation.
//
// Declared as an interface HERE, in the domain, so the HTTP layer can classify
// a provider rejection without importing the WhatsApp client. The concrete type
// lives in infra/conversation/whatsapp; delivery only ever sees this shape,
// which is the direction dependencies are supposed to run.
type ProviderError interface {
	error
	// UserMessage is the provider's own end-user sentence, already localised by
	// them. Empty when the provider sent nothing usable.
	UserMessage() string
	// ProviderUnavailable separates "the provider is down or throttling us",
	// which is ours to retry, from "the provider refused this template", which
	// is the operator's to fix.
	ProviderUnavailable() bool
}

// AsProviderError extracts a ProviderError from an error chain.
func AsProviderError(err error) (ProviderError, bool) {
	var pe ProviderError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}
