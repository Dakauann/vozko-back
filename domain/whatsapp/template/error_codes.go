package template

import (
	"errors"
	"sort"
)

const (
	CodeUnknown = "template_unknown_error"

	CodeNotFound              = "template_not_found"
	CodeAccessDenied          = "template_access_denied"
	CodeHeaderMediaOutside    = "template_header_media_outside_storage"
	CodePhoneOutsideWorkspace = "template_phone_outside_workspace"
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

	CodeOTPTypeRequired                 = "template_otp_type_required"
	CodeInvalidOTPType                  = "template_invalid_otp_type"
	CodeMultipleOTPButtons              = "template_multiple_otp_buttons"
	CodeOTPTypeUnsupported              = "template_otp_type_unsupported"
	CodeOTPButtonNotAuthentication      = "template_otp_button_not_authentication"
	CodeAuthenticationNeedsOTPButton    = "template_authentication_needs_otp_button"
	CodeCodeExpirationOutOfRange        = "template_code_expiration_out_of_range"
	CodeAuthenticationNoHeader          = "template_authentication_no_header"
	CodeAuthenticationBodyNotEditable   = "template_authentication_body_not_editable"
	CodeAuthenticationFooterNotEditable = "template_authentication_footer_not_editable"
	CodeAuthenticationCodeTooLong       = "template_authentication_code_too_long"
	CodeAuthenticationCodeRequired      = "template_authentication_code_required"

	CodeSendWorkspaceRequired    = "template_send_workspace_required"
	CodeSendIdempotencyRequired  = "template_send_idempotency_required"
	CodeSendInProgress           = "template_send_in_progress"
	CodeSendPhoneMismatch        = "template_send_phone_mismatch"
	CodeSendPricingUnavailable   = "template_send_pricing_unavailable"
	CodeSendNotSendable          = "template_send_not_sendable"
	CodeSendBillingNotConfigured = "template_send_billing_not_configured"
	CodeSendAttemptConflict      = "template_send_attempt_conflict"
	CodeSendParamsMismatch       = "template_send_params_mismatch"

	CodeProviderRejected    = "template_provider_rejected"
	CodeProviderUnavailable = "template_provider_unavailable"
)

var errorCodes = map[error]string{
	ErrTemplateNotFound:            CodeNotFound,
	ErrTemplateAccessDenied:        CodeAccessDenied,
	ErrHeaderMediaOutsideStorage:   CodeHeaderMediaOutside,
	ErrPhoneOutsideWorkspace:       CodePhoneOutsideWorkspace,
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

	ErrOTPTypeRequired:                 CodeOTPTypeRequired,
	ErrInvalidOTPType:                  CodeInvalidOTPType,
	ErrMultipleOTPButtons:              CodeMultipleOTPButtons,
	ErrOTPTypeUnsupported:              CodeOTPTypeUnsupported,
	ErrOTPButtonNotAuthentication:      CodeOTPButtonNotAuthentication,
	ErrAuthenticationNeedsOTPButton:    CodeAuthenticationNeedsOTPButton,
	ErrCodeExpirationOutOfRange:        CodeCodeExpirationOutOfRange,
	ErrAuthenticationNoHeader:          CodeAuthenticationNoHeader,
	ErrAuthenticationBodyNotEditable:   CodeAuthenticationBodyNotEditable,
	ErrAuthenticationFooterNotEditable: CodeAuthenticationFooterNotEditable,
	ErrAuthenticationCodeTooLong:       CodeAuthenticationCodeTooLong,
	ErrAuthenticationCodeRequired:      CodeAuthenticationCodeRequired,

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
	ErrTemplateParamsMismatch: CodeSendParamsMismatch,
}

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

type ProviderError interface {
	error
	UserMessage() string
	ProviderUnavailable() bool
}

func AsProviderError(err error) (ProviderError, bool) {
	var pe ProviderError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}
