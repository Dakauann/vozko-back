package shared

import (
	"errors"
	"reflect"
	"testing"
)

func TestEnsureDialablePhoneNumber(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"keeps a mobile that already has the ninth digit", "5511998765432", "5511998765432"},
		{"puts back the ninth digit a WhatsApp id dropped", "558494409684", "5584994409684"},
		{"puts back the ninth digit for legacy mobiles starting with 7 and 8", "551178765432", "5511978765432"},
		{"leaves an 8-digit number starting with 6 alone because it may be a new fixed line", "551163456789", "551163456789"},
		{"keeps a fixed line in international form", "551123456789", "551123456789"},
		{"reads a formatted international number", "+55 (11) 8876-5432", "5511988765432"},
		{"reads a national long-distance number with 0", "0 84 9440-9684", "5584994409684"},
		{"reads a national number with a carrier code", "0 15 84 99440-9684", "5584994409684"},
		{"drops a leading zero landline prefix after 55", "5501133762000", "551133762000"},
		{"reads area code and number without prefixes", "(84) 99440-9684", "5584994409684"},
		{"reads area code 55 of Rio Grande do Sul without mistaking it for the country", "5532123456", "555532123456"},
		{"gives an area code 55 mobile its ninth digit", "5599123456", "5555999123456"},
		{"leaves toll-free numbers as dialed", "0800 123 4567", "08001234567"},
		{"leaves emergency short codes as dialed", "190", "190"},
		{"leaves local numbers without area code as dialed", "9440-9684", "94409684"},
		{"leaves a number with an unknown area code as dialed", "5520994409684", "5520994409684"},
		{"leaves foreign numbers as dialed", "+1 (415) 555-0100", "+14155550100"},
		{"leaves feature codes and extensions as dialed", "*43", "*43"},
		{"returns empty for blank input", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := EnsureDialablePhoneNumber(tt.input)
			if actual != tt.expected {
				t.Fatalf("EnsureDialablePhoneNumber(%q) = %q, expected %q", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestEnsureDialablePhoneNumberIsStable(t *testing.T) {
	for _, input := range []string{"558494409684", "0 15 84 99440-9684", "551123456789", "0800 123 4567", "+1 415 555 0100"} {
		once := EnsureDialablePhoneNumber(input)
		if twice := EnsureDialablePhoneNumber(once); twice != once {
			t.Fatalf("%q normalized to %q, then to %q", input, once, twice)
		}
	}
}

func TestParsePhone(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected string
	}{
		{"reads a formatted international mobile", "+55 (84) 99440-9684", "5584994409684"},
		{"adds the country code to area code and number", "(84) 99440-9684", "5584994409684"},
		{"keeps an 8-digit subscriber as typed, matching covers the ninth digit", "84 9440-9684", "558494409684"},
		{"keeps a canonical number", "5511998765432", "5511998765432"},
		{"keeps a landline", "(11) 2345-6789", "551123456789"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePhone(tt.raw)
			if err != nil || got != tt.expected {
				t.Fatalf("ParsePhone(%q) = %q, %v; expected %q", tt.raw, got, err, tt.expected)
			}
			if NormalizePhone(tt.raw) != tt.expected {
				t.Fatalf("NormalizePhone(%q) = %q, expected %q", tt.raw, NormalizePhone(tt.raw), tt.expected)
			}
		})
	}
	for _, raw := range []string{"", "   ", "abc", "12345", "9440-9684", "+55 84 99440-96841234"} {
		t.Run("refuses "+raw, func(t *testing.T) {
			got, err := ParsePhone(raw)
			if !errors.Is(err, ErrInvalidPhone) || got != "" {
				t.Fatalf("ParsePhone(%q) = %q, %v; expected ErrInvalidPhone", raw, got, err)
			}
			if NormalizePhone(raw) != "" {
				t.Fatalf("NormalizePhone(%q) = %q, expected empty", raw, NormalizePhone(raw))
			}
		})
	}
}

func TestNinthDigitMatchingAndDialingAreTwoRules(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		variants []string
		dial     string
	}{
		{"a mobile with the ninth digit matches its 8-digit form", "5511998765432", []string{"5511998765432", "551198765432"}, "5511998765432"},
		{"a WhatsApp id without the ninth digit matches and dials with it", "558494409684", []string{"558494409684", "5584994409684"}, "5584994409684"},
		{"a legacy mobile starting with 7 matches and dials with the ninth digit", "551178765432", []string{"551178765432", "5511978765432"}, "5511978765432"},
		{"a subscriber starting with 6 matches both forms but dials as is, it may be a new landline", "551163456789", []string{"551163456789", "5511963456789"}, "551163456789"},
		{"a DDD 11 subscriber starting with 5 is left alone by both, the old 5 mobile range overlaps landlines", "551152345678", []string{"551152345678"}, "551152345678"},
		{"a landline has one form for both", "551123456789", []string{"551123456789"}, "551123456789"},
		{"matching takes canonical numbers only, dialing reads formatting", "+55 (84) 9440-9684", nil, "5584994409684"},
		{"blank input has no variants and dials nothing", "", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NinthDigitVariants(tt.input); !reflect.DeepEqual(got, tt.variants) {
				t.Fatalf("NinthDigitVariants(%q) = %v, expected %v", tt.input, got, tt.variants)
			}
			if got := EnsureDialablePhoneNumber(tt.input); got != tt.dial {
				t.Fatalf("EnsureDialablePhoneNumber(%q) = %q, expected %q", tt.input, got, tt.dial)
			}
		})
	}
}
