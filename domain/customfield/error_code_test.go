package customfield

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorCode(t *testing.T) {
	cases := []struct {
		err  error
		want string
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
		{fmt.Errorf("%w: %q", ErrInvalidTone, "green"), "custom_field_invalid_tone"},
		{fmt.Errorf("%w: %q", ErrToneUnknownOption, "x"), "custom_field_tone_unknown_option"},
		{fmt.Errorf("%w: %q", ErrInvalidRole, "boss"), "custom_field_invalid_role"},
		{ErrRoleRequiresSelect, "custom_field_role_requires_select"},
		{&FilterError{Key: "classificacao", Err: ErrFilterSensitive}, "custom_field_filter_sensitive_forbidden"},
		{&FilterError{Key: "x", Err: ErrFilterUnknownKey}, "custom_field_filter_unknown_key"},
		{ErrFilterOperator, "custom_field_filter_operator"},
		{ErrFilterValue, "custom_field_filter_value"},
		{fmt.Errorf("%w: leads:configure", ErrForbidden), "custom_field_forbidden"},
		{fmt.Errorf("%w: a future rule", ErrInvalidDefinition), "custom_field_invalid_definition"},
		{errors.New("db down"), ""},
		{nil, ""},
	}
	for _, tc := range cases {
		if got := ErrorCode(tc.err); got != tc.want {
			t.Errorf("ErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestErrorCode_EveryDefinitionRefusalHasItsOwnCode(t *testing.T) {
	seen := map[string]error{}
	for _, known := range errorCodes {
		if known.code == "" {
			t.Fatalf("%v has an empty code", known.err)
		}
		if previous, dup := seen[known.code]; dup {
			t.Fatalf("%v and %v share the code %q", previous, known.err, known.code)
		}
		seen[known.code] = known.err
	}
}

func TestErrorCodeOfValueRefusals(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{valueError("cpf", ErrUnknownKey), "custom_field_unknown_key"},
		{valueError("notas", ErrValueType), "custom_field_value_type"},
		{valueError("interesse", ErrValueNotInOptions), "custom_field_value_not_in_options"},
		{valueError("interesse", ErrValueRequired), "custom_field_value_required"},
		{valueError("classificacao", ErrValueForbidden), "custom_field_sensitive_forbidden"},
		{ErrDefinitionsUnavailable, "custom_field_definitions_unavailable"},
	}
	for _, tc := range cases {
		if got := ErrorCode(tc.err); got != tc.want {
			t.Errorf("ErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestValueRefusalsAreTheInputErrors(t *testing.T) {
	for _, err := range []error{ErrUnknownKey, ErrValueType, ErrValueNotInOptions, ErrValueRequired} {
		if !IsValueRefusal(valueError("k", err)) {
			t.Errorf("%v must be an input refusal", err)
		}
	}
	for _, err := range []error{ErrValueForbidden, ErrDefinitionsUnavailable, errors.New("db down")} {
		if IsValueRefusal(err) {
			t.Errorf("%v must not be an input refusal", err)
		}
	}
}
