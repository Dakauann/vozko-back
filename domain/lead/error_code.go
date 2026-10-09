package lead

import (
	"errors"

	"vozko/domain/address"
	"vozko/domain/customfield"
)

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrLeadNotFound, "lead_not_found"},
	{ErrLeadForbidden, "forbidden"},
	{ErrLeadSearchTooShort, "lead_search_too_short"},
	{ErrLeadFilterInvalid, "lead_filter_invalid"},
	{ErrLeadFilterAddressForbidden, "lead_filter_address_forbidden"},
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
	{ErrLeadRequired, "lead_required"},
	{ErrLeadSourceInvalid, "lead_source_invalid"},
	{ErrLeadConsentSourceInvalid, "lead_consent_source_invalid"},
	{ErrLeadOptOutSourceInvalid, "lead_opt_out_source_invalid"},
	{ErrPhoneInvalid, "lead_phone_invalid"},
	{ErrPhoneLabelInvalid, "lead_phone_label_invalid"},
	{ErrPhoneLimit, "lead_phone_limit"},
	{ErrPhoneRepeatsIdentity, "lead_phone_repeats_identity"},
	{ErrPhoneRepeated, "lead_phone_repeated"},
	{ErrPhoneUnknown, "lead_phone_unknown"},
	{address.ErrCEPMismatch, "lead_address_cep_mismatch"},
	{address.ErrInvalidAddress, "lead_address_invalid"},
	{ErrAddressLabelInvalid, "lead_address_label_invalid"},
	{ErrAddressLimit, "lead_address_limit"},
	{ErrAddressPrimary, "lead_address_primary"},
	{ErrAddressUnknown, "lead_address_unknown"},
	{ErrNoPrimaryAddress, "lead_no_primary_address"},
	{ErrRelationKindInvalid, "lead_relation_kind_invalid"},
	{ErrRelationSelf, "lead_relation_self"},
	{ErrRelationOtherWorkspace, "lead_relative_not_found"},
	{ErrRelativeNotFound, "lead_relative_not_found"},
	{ErrRelationExists, "lead_relation_exists"},
	{ErrRelationNotFound, "lead_relation_not_found"},
	{ErrIdentityInUse, "lead_identity_in_use"},
	{ErrGeocodeStale, "lead_geocode_stale"},
	{ErrLocationInvalid, "lead_location_invalid"},
	{ErrLocationNotFound, "lead_location_not_found"},
	{ErrAddressNotFound, "lead_address_not_found"},
	{ErrAddressesForbidden, "lead_addresses_forbidden"},
	{ErrRelativesQueryInvalid, "lead_relatives_query_invalid"},
	{ErrPageQueryInvalid, "lead_page_invalid"},
	{ErrLeadDialBlocked, "lead_blocked"},
	{ErrLeadNotDialable, "lead_not_dialable"},
	{ErrProfileEmpty, "lead_profile_empty"},
	{ErrProfileSourceInvalid, "lead_profile_source_invalid"},
	{ErrProfileCEPUnknown, "lead_cep_not_found"},
	{ErrProfileCEPUnchecked, "lead_cep_unavailable"},
}

var inputRefusals = []error{
	ErrLeadInvalid, ErrLeadIdentityRequired, ErrLeadNameTooLong, ErrLeadNicknameTooLong,
	ErrLeadEmailInvalid, ErrLeadBirthDateInvalid, ErrLeadOwnerInvalid, ErrLeadOwnerOutsideWorkspace, ErrLeadRequired,
	ErrPhoneInvalid, ErrPhoneLabelInvalid, ErrPhoneLimit, ErrPhoneRepeatsIdentity, ErrPhoneRepeated, ErrPhoneUnknown,
	address.ErrInvalidAddress, ErrAddressLabelInvalid, ErrAddressLimit, ErrAddressPrimary, ErrAddressUnknown, ErrNoPrimaryAddress,
	ErrRelationKindInvalid, ErrRelationSelf, ErrRelativesQueryInvalid, ErrPageQueryInvalid,
	ErrProfileEmpty, ErrProfileCEPUnknown, ErrLeadOptOutSourceInvalid, ErrLocationInvalid,
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
	return customfield.ErrorCode(err)
}

func IsInputRefusal(err error) bool {
	if customfield.IsValueRefusal(err) {
		return true
	}
	for _, refusal := range inputRefusals {
		if errors.Is(err, refusal) {
			return true
		}
	}
	return false
}
