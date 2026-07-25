package support_inbox

import "testing"

func TestSupportInbox_Normalize(t *testing.T) {
	inbox := &SupportInbox{
		Name:           "  My Support  ",
		WidgetColor:    "  #FF0000  ",
		AllowedOrigins: []string{"  https://example.com  ", "  http://localhost:3000  "},
	}
	inbox.Normalize()

	if inbox.Name != "My Support" {
		t.Errorf("expected trimmed name 'My Support', got %q", inbox.Name)
	}
	if inbox.WidgetColor != "#FF0000" {
		t.Errorf("expected trimmed color '#FF0000', got %q", inbox.WidgetColor)
	}
	if inbox.AllowedOrigins[0] != "https://example.com" {
		t.Errorf("expected trimmed origin, got %q", inbox.AllowedOrigins[0])
	}
}

func TestSupportInbox_Validate_NameRequired(t *testing.T) {
	inbox := &SupportInbox{Name: "", WorkspaceID: "ws-1"}
	if err := inbox.Validate(); err != ErrInboxNameRequired {
		t.Errorf("expected ErrInboxNameRequired, got %v", err)
	}
}

func TestSupportInbox_Validate_InvalidOrigin(t *testing.T) {
	inbox := &SupportInbox{
		Name:           "Test",
		WorkspaceID:    "ws-1",
		AllowedOrigins: []string{"not-a-url"},
	}
	if err := inbox.Validate(); err != ErrInboxOriginInvalid {
		t.Errorf("expected ErrInboxOriginInvalid, got %v", err)
	}
}

func TestSupportInbox_Validate_ValidOrigins(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
	}{
		{"empty origins (allow all)", nil},
		{"wildcard", []string{"*"}},
		{"https origin", []string{"https://example.com"}},
		{"http localhost", []string{"http://localhost:3000"}},
		{"multiple", []string{"https://example.com", "https://app.example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbox := &SupportInbox{
				Name:           "Test",
				WorkspaceID:    "ws-1",
				AllowedOrigins: tt.origins,
			}
			if err := inbox.Validate(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestSupportInbox_IsOriginAllowed(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
		origin  string
		want    bool
	}{
		{"no origins allows all", nil, "https://anything.com", true},
		{"empty origins allows all", []string{}, "https://anything.com", true},
		{"wildcard allows all", []string{"*"}, "https://anything.com", true},
		{"exact match", []string{"https://example.com"}, "https://example.com", true},
		{"no match", []string{"https://example.com"}, "https://other.com", false},
		{"case insensitive match", []string{"https://Example.com"}, "https://example.com", true},
		{"multiple origins match second", []string{"https://a.com", "https://b.com"}, "https://b.com", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbox := &SupportInbox{AllowedOrigins: tt.origins}
			if got := inbox.IsOriginAllowed(tt.origin); got != tt.want {
				t.Errorf("IsOriginAllowed(%q) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}
