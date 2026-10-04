package webchat

import (
	"errors"
	"strings"
	"testing"
)

func validWidget() *Widget {
	return &Widget{
		WorkspaceID:    "ws-1",
		Name:           "Loja",
		PublicKey:      "pk",
		AllowedOrigins: []string{"https://loja.example.com"},
	}
}

func TestWidgetNormalizeFillsSafeDefaults(t *testing.T) {
	w := validWidget()
	w.AllowedOrigins = []string{" https://LOJA.example.com/ ", "https://loja.example.com"}
	w.Normalize()

	if w.Status != StatusActive || w.Position != PositionRight || w.IdentityMode != IdentityOff {
		t.Fatalf("defaults = %q %q %q", w.Status, w.Position, w.IdentityMode)
	}
	if w.IntakeName != FieldOptional || w.IntakeEmail != FieldOptional || w.IntakePhone != FieldHidden {
		t.Fatalf("intake defaults = %q %q %q", w.IntakeName, w.IntakeEmail, w.IntakePhone)
	}
	if w.DefaultCountryCode != "55" || w.AccentColor != DefaultAccentColor {
		t.Fatalf("country %q colour %q", w.DefaultCountryCode, w.AccentColor)
	}
	if len(w.AllowedOrigins) != 1 || w.AllowedOrigins[0] != "https://loja.example.com" {
		t.Fatalf("origins were not canonicalised and deduplicated: %v", w.AllowedOrigins)
	}
}

func TestWidgetWithoutOriginsCannotBeSaved(t *testing.T) {
	w := validWidget()
	w.AllowedOrigins = nil
	w.Normalize()
	if err := w.Validate(); !errors.Is(err, ErrWidgetOriginsRequired) {
		t.Fatalf("Validate() = %v, want ErrWidgetOriginsRequired", err)
	}
}

func TestWidgetValidateRefusesEveryUnsafeSetting(t *testing.T) {
	cases := map[string]struct {
		mutate func(*Widget)
		want   error
	}{
		"no workspace":        {func(w *Widget) { w.WorkspaceID = "" }, ErrWorkspaceIDRequired},
		"no name":             {func(w *Widget) { w.Name = " " }, ErrWidgetNameRequired},
		"long name":           {func(w *Widget) { w.Name = strings.Repeat("a", MaxWidgetNameRunes+1) }, ErrWidgetNameTooLong},
		"wildcard origin":     {func(w *Widget) { w.AllowedOrigins = []string{"*"} }, ErrOriginInvalid},
		"plain http origin":   {func(w *Widget) { w.AllowedOrigins = []string{"http://loja.example.com"} }, ErrOriginInsecure},
		"too many origins":    {func(w *Widget) { w.AllowedOrigins = manyOrigins(MaxAllowedOrigins + 1) }, ErrWidgetTooManyOrigins},
		"bad status":          {func(w *Widget) { w.Status = "deleted" }, ErrWidgetStatusInvalid},
		"bad position":        {func(w *Widget) { w.Position = "top" }, ErrWidgetPositionInvalid},
		"bad colour":          {func(w *Widget) { w.AccentColor = "red;}" }, ErrWidgetColorInvalid},
		"bad intake rule":     {func(w *Widget) { w.IntakeEmail = "maybe" }, ErrIntakeRuleInvalid},
		"bad identity mode":   {func(w *Widget) { w.IdentityMode = "sometimes" }, ErrIdentityModeInvalid},
		"identity no secret":  {func(w *Widget) { w.IdentityMode = IdentityRequired }, ErrIdentitySecretMissing},
		"http privacy policy": {func(w *Widget) { w.PrivacyPolicyURL = "http://loja.example.com/privacidade" }, ErrPrivacyPolicyURLInvalid},
		"script privacy":      {func(w *Widget) { w.PrivacyPolicyURL = "javascript:alert(1)" }, ErrPrivacyPolicyURLInvalid},
		"bad country code":    {func(w *Widget) { w.DefaultCountryCode = "+55" }, ErrCountryCodeInvalid},
		"long welcome":        {func(w *Widget) { w.WelcomeMessage = strings.Repeat("a", MaxWelcomeMessageRunes+1) }, ErrWidgetTextTooLong},
		"no public key":       {func(w *Widget) { w.PublicKey = "" }, ErrWidgetPublicKeyMissing},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := validWidget()
			w.Normalize()
			tc.mutate(w)
			if err := w.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestFrameAncestorsListsOnlyTheWidgetOrigins(t *testing.T) {
	w := validWidget()
	w.AllowedOrigins = []string{"https://loja.example.com", "https://*.example.org"}
	w.Normalize()
	if got, want := w.FrameAncestors(), "https://loja.example.com https://*.example.org"; got != want {
		t.Fatalf("FrameAncestors() = %q, want %q", got, want)
	}
}

func TestFrameAncestorsIsNoneWhenNothingIsValid(t *testing.T) {
	w := &Widget{AllowedOrigins: []string{"*", "http://evil.example"}}
	if got := w.FrameAncestors(); got != "'none'" {
		t.Fatalf("FrameAncestors() = %q, want 'none'", got)
	}
}

func TestAllowsOriginRefusesWhenTheListIsEmpty(t *testing.T) {
	w := &Widget{}
	if w.AllowsOrigin("https://loja.example.com") {
		t.Fatal("an empty allow-list must refuse every origin")
	}
}

func TestPausedWidgetIsNotServed(t *testing.T) {
	w := validWidget()
	w.Normalize()
	w.Status = StatusPaused
	if w.Serves() {
		t.Fatal("a paused widget must not be served")
	}
}

func TestAutomationCarriesTheWidgetSettings(t *testing.T) {
	agent := "agent-1"
	w := validWidget()
	w.AgentID = &agent
	w.EnableAgentResponses = true
	w.EnableAnalysis = true
	got := w.Automation()
	if got.AgentID == nil || *got.AgentID != agent || !got.EnableAgentResponses || !got.EnableAnalysis || got.EnableWorkflow {
		t.Fatalf("Automation() = %+v", got)
	}
}

func manyOrigins(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "https://s" + strings.Repeat("x", i+1) + ".example.com"
	}
	return out
}
