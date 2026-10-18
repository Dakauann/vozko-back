package leadimport

import "slices"

var consentFields = []string{FieldConsentDate, FieldConsentPurpose}

func (s Settings) GrantsConsent() bool {
	return s.MapsAny(consentFields...)
}

func GrantsConsentField(f Field) bool {
	return slices.Contains(consentFields, f.Key)
}
