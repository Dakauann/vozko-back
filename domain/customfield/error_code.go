package customfield

import "errors"

const CodeInvalidDefinition = "custom_field_invalid_definition"

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrNotFound, "custom_field_not_found"},
	{ErrKeyExists, "custom_field_key_exists"},
	{ErrRoleTaken, "custom_field_role_taken"},
	{ErrWorkspaceRequired, "custom_field_workspace_required"},
	{ErrInvalidObjectType, "custom_field_invalid_object_type"},
	{ErrKeyRequired, "custom_field_key_required"},
	{ErrLabelRequired, "custom_field_label_required"},
	{ErrInvalidType, "custom_field_invalid_type"},
	{ErrOptionsRequired, "custom_field_options_required"},
	{ErrSensitivityChoiceMissing, "custom_field_sensitivity_choice_missing"},
	{ErrLegalBasisRequired, "custom_field_legal_basis_required"},
	{ErrLegalBasisTooLong, "custom_field_legal_basis_too_long"},
	{ErrSensitiveUnsupportedObject, "custom_field_sensitive_unsupported_object"},
	{ErrInvalidTone, "custom_field_invalid_tone"},
	{ErrToneUnknownOption, "custom_field_tone_unknown_option"},
	{ErrInvalidRole, "custom_field_invalid_role"},
	{ErrRoleRequiresSelect, "custom_field_role_requires_select"},
	{ErrUnknownKey, "custom_field_unknown_key"},
	{ErrValueType, "custom_field_value_type"},
	{ErrValueNotInOptions, "custom_field_value_not_in_options"},
	{ErrValueRequired, "custom_field_value_required"},
	{ErrValueForbidden, "custom_field_sensitive_forbidden"},
	{ErrDefinitionsUnavailable, "custom_field_definitions_unavailable"},
	{ErrFilterSensitive, "custom_field_filter_sensitive_forbidden"},
	{ErrFilterUnknownKey, "custom_field_filter_unknown_key"},
	{ErrFilterOperator, "custom_field_filter_operator"},
	{ErrFilterValue, "custom_field_filter_value"},
	{ErrForbidden, "custom_field_forbidden"},
}

var valueRefusals = []error{ErrUnknownKey, ErrValueType, ErrValueNotInOptions, ErrValueRequired}

func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	if errors.Is(err, ErrInvalidDefinition) {
		return CodeInvalidDefinition
	}
	return ""
}

func IsValueRefusal(err error) bool {
	for _, refusal := range valueRefusals {
		if errors.Is(err, refusal) {
			return true
		}
	}
	return false
}
