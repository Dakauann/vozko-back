package lead

import (
	"errors"
	"fmt"
	"testing"

	"vozko/domain/address"
	"vozko/domain/customfield"
)

func TestErrorCode(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{ErrLeadNotFound, "lead_not_found"},
		{fmt.Errorf("find lead: %w", ErrLeadNotFound), "lead_not_found"},
		{ErrLeadForbidden, "forbidden"},
		{fmt.Errorf("%w: %w", ErrLeadFilterInvalid, errors.New("bad predicate")), "lead_filter_invalid"},
		{ErrLeadDuplicate, "lead_identity_taken"},
		{ErrLeadInvalid, "lead_number_invalid"},
		{ErrLeadIdentityRequired, "lead_identity_required"},
		{ErrLeadNameTooLong, "lead_name_too_long"},
		{ErrLeadNicknameTooLong, "lead_nickname_too_long"},
		{ErrLeadEmailInvalid, "lead_email_invalid"},
		{ErrLeadBirthDateInvalid, "lead_birth_date_invalid"},
		{ErrLeadOwnerInvalid, "lead_owner_invalid"},
		{ErrLeadOwnerOutsideWorkspace, "lead_owner_outside_workspace"},
		{ErrLeadOwnerOutOfReach, "lead_owner_out_of_reach"},
		{ErrLeadSourceInvalid, "lead_source_invalid"},
		{ErrLeadConsentSourceInvalid, "lead_consent_source_invalid"},
		{errors.New("database down"), ""},
	}
	for _, tc := range cases {
		if got := ErrorCode(tc.err); got != tc.want {
			t.Errorf("ErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestEveryInputRefusalIsABadRequest(t *testing.T) {
	for _, err := range []error{ErrLeadInvalid, ErrLeadIdentityRequired, ErrLeadNameTooLong, ErrLeadNicknameTooLong, ErrLeadEmailInvalid, ErrLeadBirthDateInvalid, ErrLeadOwnerInvalid, ErrLeadOwnerOutsideWorkspace, ErrLeadRequired} {
		if !IsInputRefusal(err) {
			t.Errorf("%v must be an input refusal", err)
		}
	}
	for _, err := range []error{ErrLeadNotFound, ErrLeadForbidden, ErrLeadDuplicate, ErrLeadOwnerOutOfReach, ErrLeadSourceInvalid, ErrLeadConsentSourceInvalid, errors.New("x")} {
		if IsInputRefusal(err) {
			t.Errorf("%v must not be an input refusal", err)
		}
	}
}

func TestCollectionErrorCodes(t *testing.T) {
	cases := []struct {
		err   error
		want  string
		input bool
	}{
		{ErrPhoneInvalid, "lead_phone_invalid", true},
		{ErrPhoneLabelInvalid, "lead_phone_label_invalid", true},
		{ErrPhoneLimit, "lead_phone_limit", true},
		{ErrPhoneRepeatsIdentity, "lead_phone_repeats_identity", true},
		{ErrPhoneRepeated, "lead_phone_repeated", true},
		{ErrPhoneUnknown, "lead_phone_unknown", true},
		{address.InvalidFieldError{Field: address.FieldZipCode, Rule: address.RuleFormat}, "lead_address_invalid", true},
		{ErrAddressLabelInvalid, "lead_address_label_invalid", true},
		{ErrAddressLimit, "lead_address_limit", true},
		{ErrAddressPrimary, "lead_address_primary", true},
		{ErrAddressUnknown, "lead_address_unknown", true},
		{ErrNoPrimaryAddress, "lead_no_primary_address", true},
		{ErrRelationKindInvalid, "lead_relation_kind_invalid", true},
		{ErrRelationSelf, "lead_relation_self", true},
		{ErrRelationOtherWorkspace, "lead_relative_not_found", false},
		{ErrRelativeNotFound, "lead_relative_not_found", false},
		{ErrRelationExists, "lead_relation_exists", false},
		{ErrRelationNotFound, "lead_relation_not_found", false},
		{ErrIdentityInUse, "lead_identity_in_use", false},
		{ErrGeocodeStale, "lead_geocode_stale", false},
		{ErrLocationInvalid, "lead_location_invalid", true},
		{ErrLocationNotFound, "lead_location_not_found", false},
		{ErrAddressNotFound, "lead_address_not_found", false},
		{ErrIdentityUnchecked, "", false},
		{&ItemError{Field: FieldPhones, Index: 2, Err: ErrPhoneRepeated}, "lead_phone_repeated", true},
		{&IdentityTaken{LeadID: "l-1"}, "lead_identity_taken", false},
	}
	for _, tc := range cases {
		if got := ErrorCode(tc.err); got != tc.want {
			t.Errorf("ErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
		if got := IsInputRefusal(tc.err); got != tc.input {
			t.Errorf("IsInputRefusal(%v) = %v, want %v", tc.err, got, tc.input)
		}
	}
}

func TestCustomFieldRefusalsKeepTheirCodes(t *testing.T) {
	cases := []struct {
		err   error
		code  string
		input bool
	}{
		{&customfield.ValueError{Key: "interesse", Err: customfield.ErrValueNotInOptions}, "custom_field_value_not_in_options", true},
		{&customfield.ValueError{Key: "cpf", Err: customfield.ErrUnknownKey}, "custom_field_unknown_key", true},
		{&customfield.ValueError{Key: "classificacao", Err: customfield.ErrValueForbidden}, "custom_field_sensitive_forbidden", false},
		{ErrAddressesForbidden, "lead_addresses_forbidden", false},
		{ErrRelativesQueryInvalid, "lead_relatives_query_invalid", true},
		{ErrCustomFieldsUnchecked, "", false},
	}
	for _, tc := range cases {
		if got := ErrorCode(tc.err); got != tc.code {
			t.Errorf("ErrorCode(%v) = %q, want %q", tc.err, got, tc.code)
		}
		if got := IsInputRefusal(tc.err); got != tc.input {
			t.Errorf("IsInputRefusal(%v) = %v, want %v", tc.err, got, tc.input)
		}
	}
}
