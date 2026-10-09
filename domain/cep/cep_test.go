package cep

import (
	"errors"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected string
		valid    bool
	}{
		{"keeps eight digits", "01310100", "01310100", true},
		{"drops the hyphen", "01310-100", "01310100", true},
		{"drops the dot of the old mask", "01.310-100", "01310100", true},
		{"drops surrounding and inner spaces", " 01310 100 ", "01310100", true},
		{"refuses seven digits", "0131010", "", false},
		{"refuses nine digits", "013101000", "", false},
		{"refuses letters instead of silently dropping them", "CEP 01310-100", "", false},
		{"refuses a slash", "01310/100", "", false},
		{"refuses blank input", "   ", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.raw)
			if tt.valid {
				if err != nil || got != tt.expected {
					t.Fatalf("Parse(%q) = %q, %v; expected %q", tt.raw, got, err, tt.expected)
				}
				return
			}
			if !errors.Is(err, ErrInvalidCEP) || got != "" {
				t.Fatalf("Parse(%q) = %q, %v; expected ErrInvalidCEP", tt.raw, got, err)
			}
		})
	}
}

func TestFormat(t *testing.T) {
	if got := Format("01310100"); got != "01310-100" {
		t.Fatalf("Format = %q", got)
	}
	if got := Format("0131"); got != "0131" {
		t.Fatalf("Format of an unparsed code must leave it alone, got %q", got)
	}
}

func TestIsGeneric(t *testing.T) {
	tests := []struct {
		code    string
		generic bool
	}{
		{"69300000", true},
		{"39270-000", true},
		{"01310100", false},
		{"30140071", false},
		{"invalid", false},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			if got := IsGeneric(tt.code); got != tt.generic {
				t.Fatalf("IsGeneric(%q) = %v, expected %v", tt.code, got, tt.generic)
			}
		})
	}
}

func TestNeedsCityCode(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		info CEPInfo
		want bool
	}{
		{"a row with its city code is complete", CEPInfo{IBGE: "3550308"}, false},
		{"a row never checked is refreshed", CEPInfo{}, true},
		{"a row checked within the interval waits", CEPInfo{CheckedAt: now.Add(-CityCodeRetryInterval + time.Minute)}, false},
		{"a row checked an interval ago is refreshed", CEPInfo{CheckedAt: now.Add(-CityCodeRetryInterval)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.NeedsCityCode(now); got != tt.want {
				t.Fatalf("NeedsCityCode = %v, expected %v", got, tt.want)
			}
		})
	}
}
