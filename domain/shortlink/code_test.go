package shortlink

import (
	"errors"
	"strings"
	"testing"
)

type failingReader struct{}

func (failingReader) Read(p []byte) (int, error) { return 0, errors.New("rand failure") }

func TestClampCodeLength(t *testing.T) {
	if got := ClampCodeLength(MinCodeLength - 1); got != DefaultCodeLength {
		t.Fatalf("below min = %d want %d", got, DefaultCodeLength)
	}
	if got := ClampCodeLength(MaxCodeLength + 1); got != MaxCodeLength {
		t.Fatalf("above max = %d want %d", got, MaxCodeLength)
	}
	if got := ClampCodeLength(8); got != 8 {
		t.Fatalf("in range = %d want 8", got)
	}
}

func TestGenerateCode(t *testing.T) {
	code, err := GenerateCode(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(code) != 10 {
		t.Fatalf("len = %d want 10", len(code))
	}
	for _, r := range code {
		if !strings.ContainsRune(base62Alphabet, r) {
			t.Fatalf("char %q not in base62 alphabet", r)
		}
	}
}

func TestGenerateCodeError(t *testing.T) {
	orig := randReader
	randReader = failingReader{}
	defer func() { randReader = orig }()

	if _, err := GenerateCode(7); err == nil {
		t.Fatal("expected error from failing reader")
	}
}

func TestNormalizeCode(t *testing.T) {
	if got := NormalizeCode("  abc  "); got != "abc" {
		t.Fatalf("NormalizeCode = %q", got)
	}
}

func TestIsReservedCode(t *testing.T) {
	if !IsReservedCode("api") || !IsReservedCode("  ADMIN ") {
		t.Fatal("reserved codes must be detected case-insensitively")
	}
	if IsReservedCode("promo-julho") {
		t.Fatal("non-reserved code flagged")
	}
}

func TestValidateCustomAlias(t *testing.T) {
	tests := []struct {
		alias   string
		wantErr error
	}{
		{"promo_julho-1", nil},
		{"", ErrCodeRequired},
		{"ab", ErrInvalidAliasLength},
		{strings.Repeat("a", MaxCodeLength+1), ErrInvalidAliasLength},
		{"bad space", ErrInvalidAliasChar},
		{"has/slash", ErrInvalidAliasChar},
		{"admin", ErrReservedAlias},
	}
	for _, tt := range tests {
		if err := ValidateCustomAlias(tt.alias); err != tt.wantErr {
			t.Fatalf("ValidateCustomAlias(%q) = %v want %v", tt.alias, err, tt.wantErr)
		}
	}
}

func TestShortURL(t *testing.T) {
	if got := ShortURL("https://vx.co/r/", "abc"); got != "https://vx.co/r/abc" {
		t.Fatalf("trailing slash = %q", got)
	}
	if got := ShortURL("https://vx.co/r", "abc"); got != "https://vx.co/r/abc" {
		t.Fatalf("no slash = %q", got)
	}
}
