package address

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"vozko/domain/cep"
	"vozko/domain/shared"
)

var ErrInvalidPlace = errors.New("address: a place needs a 7 digit city code and a district")

const (
	MaxStreetLength      = 200
	MaxNumberLength      = 20
	MaxComplementLength  = 100
	MaxDistrictLength    = 100
	MaxCityLength        = 100
	cityCodeLength       = 7
	fingerprintLength    = 32
	fingerprintSeparator = "\x1f"
)

type Field string

const (
	FieldName       Field = "name"
	FieldZipCode    Field = "zipCode"
	FieldStreet     Field = "street"
	FieldNumber     Field = "number"
	FieldComplement Field = "complement"
	FieldDistrict   Field = "district"
	FieldCity       Field = "city"
	FieldState      Field = "state"
	FieldCityCode   Field = "cityCode"
)

type Rule string

const (
	RuleFormat            Rule = "format"
	RuleTooLong           Rule = "too_long"
	RuleUnknownState      Rule = "unknown_state"
	RuleZipOrCityRequired Rule = "zip_or_city_required"
)

type InvalidFieldError struct {
	Field Field
	Rule  Rule
}

func (e InvalidFieldError) Error() string {
	return fmt.Sprintf("%s: %s %s", ErrInvalidAddress, e.Field, e.Rule)
}

func (e InvalidFieldError) Unwrap() error {
	return ErrInvalidAddress
}

var brazilStates = map[string]bool{
	"AC": true, "AL": true, "AP": true, "AM": true, "BA": true, "CE": true, "DF": true,
	"ES": true, "GO": true, "MA": true, "MT": true, "MS": true, "MG": true, "PA": true,
	"PB": true, "PR": true, "PE": true, "PI": true, "RJ": true, "RN": true, "RS": true,
	"RO": true, "RR": true, "SC": true, "SP": true, "SE": true, "TO": true,
}

type Postal struct {
	ZipCode    string `json:"zipCode"`
	Street     string `json:"street"`
	Number     string `json:"number"`
	Complement string `json:"complement,omitempty"`
	District   string `json:"district"`
	City       string `json:"city"`
	State      string `json:"state"`
	CityCode   string `json:"cityCode,omitempty"`
}

type Place struct {
	CityCode    string
	DistrictKey string
}

func (p Postal) Normalize() Postal {
	zip := collapse(p.ZipCode)
	if parsed, err := cep.Parse(zip); err == nil {
		zip = parsed
	}
	return Postal{
		ZipCode:    zip,
		Street:     collapse(p.Street),
		Number:     collapse(p.Number),
		Complement: collapse(p.Complement),
		District:   collapse(p.District),
		City:       collapse(p.City),
		State:      normalizeState(p.State),
		CityCode:   collapse(p.CityCode),
	}
}

func normalizeState(raw string) string {
	if code, ok := StateCode(raw); ok {
		return code
	}
	return strings.ToUpper(collapse(raw))
}

func (p Postal) Validate() error {
	n := p.Normalize()
	if n.ZipCode != "" {
		if _, err := cep.Parse(n.ZipCode); err != nil {
			return InvalidFieldError{Field: FieldZipCode, Rule: RuleFormat}
		}
	}
	if n.CityCode != "" && !ValidCityCode(n.CityCode) {
		return InvalidFieldError{Field: FieldCityCode, Rule: RuleFormat}
	}
	if n.State != "" && !brazilStates[n.State] {
		return InvalidFieldError{Field: FieldState, Rule: RuleUnknownState}
	}
	for _, limit := range []struct {
		field Field
		value string
		max   int
	}{
		{FieldStreet, n.Street, MaxStreetLength},
		{FieldNumber, n.Number, MaxNumberLength},
		{FieldComplement, n.Complement, MaxComplementLength},
		{FieldDistrict, n.District, MaxDistrictLength},
		{FieldCity, n.City, MaxCityLength},
	} {
		if utf8.RuneCountInString(limit.value) > limit.max {
			return InvalidFieldError{Field: limit.field, Rule: RuleTooLong}
		}
	}
	if n.ZipCode == "" && (n.City == "" || n.State == "") {
		return InvalidFieldError{Field: FieldZipCode, Rule: RuleZipOrCityRequired}
	}
	return nil
}

func (p Postal) Missing() []Field {
	var missing []Field
	for _, field := range []struct {
		name  Field
		value string
	}{
		{FieldZipCode, p.ZipCode},
		{FieldStreet, p.Street},
		{FieldNumber, p.Number},
		{FieldDistrict, p.District},
		{FieldCity, p.City},
		{FieldState, p.State},
	} {
		if strings.TrimSpace(field.value) == "" {
			missing = append(missing, field.name)
		}
	}
	return missing
}

func (p Postal) Fingerprint() string {
	n := p.Normalize()
	parts := []string{n.ZipCode, n.Street, n.Number, n.District, n.City, n.State}
	for i, part := range parts {
		parts[i] = shared.FoldForMatch(part)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, fingerprintSeparator)))
	return hex.EncodeToString(sum[:])[:fingerprintLength]
}

func (p Postal) Place() Place {
	return Place{CityCode: collapse(p.CityCode), DistrictKey: DistrictKey(p.District)}
}

func (p Place) Validate() error {
	if !ValidCityCode(p.CityCode) || p.DistrictKey == "" {
		return ErrInvalidPlace
	}
	return nil
}

func collapse(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func ValidCityCode(value string) bool {
	if len(value) != cityCodeLength {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
