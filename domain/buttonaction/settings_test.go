package buttonaction

import (
	"errors"
	"strings"
	"testing"
)

func TestATemplateExitConfirmationIsTrimmedAndBounded(t *testing.T) {
	confirmation, err := NewExitConfirmation("  Pronto, você saiu.  ")
	if err != nil || confirmation != "Pronto, você saiu." {
		t.Fatalf("confirmation = %q, %v", confirmation, err)
	}
	if blank, err := NewExitConfirmation("   "); err != nil || blank != "" {
		t.Fatalf("a blank confirmation sends none: %q, %v", blank, err)
	}
	if _, err := NewExitConfirmation(strings.Repeat("a", MaxConfirmationLength+1)); !errors.Is(err, ErrExitConfirmationLong) {
		t.Fatalf("got %v, want ErrExitConfirmationLong", err)
	}
}

func TestNormalizedSettingsStartAtTheFirstVersion(t *testing.T) {
	s := TemplateSettings{}.Normalized()
	if s.Version != FirstVersion || s.Actions == nil {
		t.Fatalf("settings = %+v", s)
	}
	r := CampaignRoutes{}.Normalized()
	if r.Version != FirstVersion || r.Routes == nil {
		t.Fatalf("routes = %+v", r)
	}
}
