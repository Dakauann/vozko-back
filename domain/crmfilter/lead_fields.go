package crmfilter

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/address"
	"vozko/domain/cep"
	"vozko/domain/shared"
)

const (
	FieldID             Field = "id"
	FieldPhoneAny       Field = "phone_any"
	FieldEmail          Field = "email"
	FieldNickname       Field = "nickname"
	FieldBirthday       Field = "birthday"
	FieldBirthDate      Field = "birth_date"
	FieldZip            Field = "zip"
	FieldState          Field = "state"
	FieldCity           Field = "city"
	FieldDistrict       Field = "district"
	FieldGeoPrecision   Field = "geo_precision"
	FieldGeoStatus      Field = "geo_status"
	FieldHasAddress     Field = "has_address"
	FieldHasIdentity    Field = "has_identity"
	FieldOptedOut       Field = "opted_out"
	FieldWhatsAppOptIn  Field = "whatsapp_opt_in"
	FieldRelationKind   Field = "relation_kind"
	FieldRelativesCount Field = "relatives_count"
	FieldReferredCount  Field = "referred_count"
	FieldReferredBy     Field = "referred_by"
	FieldGeoPlacement   Field = "geo_placement"

	FieldAreaApproximate Field = "area_approximate"
)

const (
	MaxDistrictPairs = 200
	MaxZipValues     = 200
	MaxAreas         = 20
)

const districtPairSeparator = "/"

var (
	ErrInvalidValue        = errors.New("crmfilter: value cannot match this field")
	ErrTooManyValues       = errors.New("crmfilter: too many values in one predicate")
	ErrConjunctionRequired = errors.New("crmfilter: a selection needs every group to state and or or")
)

var (
	exactIDOps   = []Operator{OpIn, OpNotIn, OpEquals, OpNotEquals}
	nicknameOps  = []Operator{OpContains, OpIsSet, OpIsEmpty}
	birthdayOps  = []Operator{OpEquals, OpIn}
	areaOps      = []Operator{OpIn}
	placementOps = []Operator{OpIn, OpNotIn, OpEquals, OpNotEquals}
	leadFieldSet = []FieldSpec{
		{FieldID, KindIDSet, exactIDOps, true},
		{FieldPhoneAny, KindString, exactIDOps, true},
		{FieldEmail, KindString, stringOps, true},
		{FieldNickname, KindText, nicknameOps, false},
		{FieldBirthday, KindEnum, birthdayOps, true},
		{FieldBirthDate, KindDate, dateOps, false},
		{FieldZip, KindIDSet, idSetOps, true},
		{FieldState, KindIDSet, idSetOps, true},
		{FieldCity, KindIDSet, idSetOps, true},
		{FieldDistrict, KindIDSet, idSetOps, true},
		{FieldGeoPrecision, KindEnum, enumOps, true},
		{FieldGeoStatus, KindEnum, enumOps, true},
		{FieldHasAddress, KindBool, boolOps, false},
		{FieldHasIdentity, KindBool, boolOps, false},
		{FieldOptedOut, KindBool, boolOps, false},
		{FieldWhatsAppOptIn, KindBool, boolOps, false},
		{FieldRelationKind, KindEnum, enumOps, true},
		{FieldRelativesCount, KindNumber, numberOps, false},
		{FieldReferredCount, KindNumber, numberOps, false},
		{FieldReferredBy, KindIDSet, exactIDOps, true},
		{FieldArea, KindIDSet, areaOps, true},
		{FieldAreaApproximate, KindIDSet, areaOps, true},
		{FieldGeoPlacement, KindEnum, placementOps, true},
	}
)

var valueRules = map[Field]func(string) error{
	FieldID:              uuidValue,
	FieldReferredBy:      uuidValue,
	FieldArea:            uuidValue,
	FieldAreaApproximate: uuidValue,
	FieldGeoPlacement:    geoPlacementValue,
	FieldPhoneAny:        phoneValue,
	FieldBirthday:        birthdayValue,
	FieldZip:             zipValue,
	FieldState:           stateValue,
	FieldCity:            cityKeyValue,
	FieldDistrict:        districtValue,
}

var valueCaps = map[Field]int{
	FieldDistrict:        MaxDistrictPairs,
	FieldZip:             MaxZipValues,
	FieldArea:            MaxAreas,
	FieldAreaApproximate: MaxAreas,
}

func init() {
	for _, spec := range leadFieldSet {
		registry[spec.Field] = spec
	}
}

func checkValues(field Field, values []string) error {
	present := nonEmpty(values)
	if limit, capped := valueCaps[field]; capped && len(present) > limit {
		return fmt.Errorf("%w: %q takes at most %d values", ErrTooManyValues, field, limit)
	}
	rule, ok := valueRules[field]
	if !ok {
		return nil
	}
	for _, v := range present {
		if err := rule(strings.TrimSpace(v)); err != nil {
			return fmt.Errorf("%w: %q on %q", err, v, field)
		}
	}
	return nil
}

func uuidValue(v string) error {
	if _, err := uuid.Parse(v); err != nil || len(v) != 36 {
		return ErrInvalidValue
	}
	return nil
}

func phoneValue(v string) error {
	if _, err := shared.ParsePhone(v); err != nil {
		return ErrInvalidValue
	}
	return nil
}

func zipValue(v string) error {
	if _, err := cep.Parse(v); err != nil {
		return ErrInvalidValue
	}
	return nil
}

func stateValue(v string) error {
	if len(v) != 2 || !letters(v) {
		return ErrInvalidValue
	}
	return nil
}

func cityKeyValue(v string) error {
	_, err := ParseCityKey(v)
	return err
}

func ParseCityKey(raw string) (string, error) {
	if strings.Contains(raw, districtPairSeparator) {
		return "", ErrInvalidValue
	}
	key, ok := address.ParseCityKey(raw)
	if !ok {
		return "", ErrInvalidValue
	}
	return key, nil
}

func districtValue(v string) error {
	_, _, err := ParseDistrictPair(v)
	return err
}

func letters(v string) bool {
	for _, r := range v {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

func ParseDistrictPair(raw string) (cityKey, districtKey string, err error) {
	parts := strings.Split(strings.TrimSpace(raw), districtPairSeparator)
	if len(parts) != 2 {
		return "", "", ErrInvalidValue
	}
	cityKey, err = ParseCityKey(parts[0])
	districtKey = address.DistrictKey(parts[1])
	if err != nil || districtKey == "" {
		return "", "", ErrInvalidValue
	}
	return cityKey, districtKey, nil
}

func DistrictPair(cityKey, districtKey string) string {
	return cityKey + districtPairSeparator + districtKey
}

func PhoneAnyNumbers(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	for _, raw := range nonEmpty(values) {
		number, err := shared.ParsePhone(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %q is not a phone", ErrInvalidValue, raw)
		}
		for _, variant := range shared.NinthDigitVariants(number) {
			if _, dup := seen[variant]; dup {
				continue
			}
			seen[variant] = struct{}{}
			out = append(out, variant)
		}
	}
	return out, nil
}

func (f Filter) UsesField(field Field) bool {
	for _, g := range f.Groups {
		for _, p := range g.Predicates {
			if p.Field == field {
				return true
			}
		}
	}
	return false
}

func (f Filter) ValidateForSelection() error {
	if err := f.Validate(); err != nil {
		return err
	}
	for gi, g := range f.Groups {
		if len(g.Predicates) == 0 {
			continue
		}
		if g.Conjunction != And && g.Conjunction != Or {
			return fmt.Errorf("group %d: %w", gi, ErrConjunctionRequired)
		}
	}
	return nil
}

type GeoPlacement string

const (
	PlacementOnMap          GeoPlacement = "on_map"
	PlacementApproximate    GeoPlacement = "approximate"
	PlacementWithoutAddress GeoPlacement = "without_address"
	PlacementNotFound       GeoPlacement = "not_found"
	PlacementPending        GeoPlacement = "pending"
	PlacementQuotaExceeded  GeoPlacement = "quota_exceeded"
	PlacementRefused        GeoPlacement = "refused"
)

var geoPlacements = []GeoPlacement{
	PlacementOnMap, PlacementApproximate, PlacementWithoutAddress, PlacementNotFound,
	PlacementPending, PlacementQuotaExceeded, PlacementRefused,
}

func GeoPlacements() []GeoPlacement { return append([]GeoPlacement(nil), geoPlacements...) }

func (p GeoPlacement) Valid() bool {
	for _, known := range geoPlacements {
		if p == known {
			return true
		}
	}
	return false
}

func geoPlacementValue(v string) error {
	if !GeoPlacement(v).Valid() {
		return ErrInvalidValue
	}
	return nil
}

const AreaExactOnly = "exact_only"

var ErrAreaMembershipInvalid = fmt.Errorf("%w: unknown area membership", ErrInvalidValue)

func AreaExactOnlyOf(p Predicate) bool {
	return p.Field == FieldArea && strings.TrimSpace(p.Key) == AreaExactOnly
}

func AreaPlacements(p Predicate) ([]GeoPlacement, error) {
	key := strings.TrimSpace(p.Key)
	switch {
	case p.Field == FieldArea && key == "":
		return []GeoPlacement{PlacementOnMap, PlacementApproximate}, nil
	case p.Field == FieldArea && key == AreaExactOnly:
		return []GeoPlacement{PlacementOnMap}, nil
	case p.Field == FieldAreaApproximate && key == "":
		return []GeoPlacement{PlacementApproximate}, nil
	}
	return nil, fmt.Errorf("%w: key %q on %q", ErrAreaMembershipInvalid, p.Key, p.Field)
}
