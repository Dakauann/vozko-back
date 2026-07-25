package shortlink

import (
	"strings"
	"testing"
)

func TestValidateTargetURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr error
	}{
		{"valid https", "https://example.com/path?q=1", nil},
		{"valid http", "http://example.com", nil},
		{"empty", "   ", ErrTargetURLRequired},
		{"too long", "https://example.com/" + strings.Repeat("a", MaxTargetURLLength), ErrTargetURLTooLong},
		{"bad scheme ftp", "ftp://example.com", ErrTargetURLScheme},
		{"bad scheme js", "javascript:alert(1)", ErrTargetURLScheme},
		{"missing host", "https://", ErrTargetURLInvalid},
		{"credentials", "https://user:pass@example.com", ErrTargetURLCredentials},
		{"unparseable", "https://exa mple.com/\x7f", ErrTargetURLInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateTargetURL(tt.url); err != tt.wantErr {
				t.Fatalf("ValidateTargetURL(%q) = %v want %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestTargetHost(t *testing.T) {
	if got := TargetHost("https://example.com:443/x"); got != "example.com" {
		t.Fatalf("host = %q", got)
	}
	if got := TargetHost("://%zz"); got != "" {
		t.Fatalf("invalid url host = %q", got)
	}
}
