package lead

import (
	"testing"
)

func TestNormalizeNumber_BrazilianFormats(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{

		{"12-digit Brazilian mobile", "551194219642", "551194219642"},

		{"13-digit Brazilian mobile with 9th digit", "5511994219642", "5511994219642"},

		{"11-digit too short", "55114219642", ""},

		{"plus prefix", "+551194219642", ""},

		{"12-digit Brazilian landline", "551133334444", "551133334444"},

		{"empty string", "", ""},
		{"whitespace only", "  ", ""},

		{"US number", "12125551234", ""},

		{"too short", "55119", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeNumber(tt.input)
			if result != tt.expect {
				t.Errorf("NormalizeNumber(%q) = %q, want %q", tt.input, result, tt.expect)
			}
		})
	}
}

func TestNormalizeNumber_MigrationImplications(t *testing.T) {
	formats := []struct {
		stored string
		valid  bool
	}{
		{"551194219642", true},
		{"5511994219642", true},
		{"+551194219642", false},
		{"1194219642", false},
		{"55114219642", false},
	}

	for _, f := range formats {
		result := NormalizeNumber(f.stored)
		if f.valid && result == "" {
			t.Errorf("stored %q should be valid but NormalizeNumber returned empty", f.stored)
		}
		if !f.valid && result != "" {
			t.Errorf("stored %q should be invalid but NormalizeNumber returned %q", f.stored, result)
		}
	}
}
