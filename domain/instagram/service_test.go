package instagram

import (
	"strings"
	"testing"
)

// TestValidateRedirectURI covers the class of bug that produced a real
// "Invalid redirect_uri": the configured URI disagreeing with the path this build
// actually serves. The path is a code constant, so only the host is configurable,
// and everything else must fail at boot rather than mid-onboarding.
func TestValidateRedirectURI(t *testing.T) {
	valid := []string{
		"https://homolog-api.vozkoia.com" + OAuthCallbackPath,
		// A trailing slash is tolerated: Meta's dashboard is documented to append
		// one sometimes, and the router registers both spellings.
		"https://homolog-api.vozkoia.com" + OAuthCallbackPath + "/",
		// http is allowed on localhost for local development.
		"http://localhost:4000" + OAuthCallbackPath,
		"http://127.0.0.1:4000" + OAuthCallbackPath,
	}
	for _, uri := range valid {
		if err := ValidateRedirectURI(uri); err != nil {
			t.Errorf("ValidateRedirectURI(%q) = %v, want nil", uri, err)
		}
	}

	invalid := []struct {
		uri  string
		want string
	}{
		{"", "required"},
		{"   ", "required"},
		// The exact mistake that was in .env: the webhook URL, not the callback.
		{"https://homolog-api.vozkoia.com/webhooks/instagram", "path"},
		// The exact mistake that was in the router: reversed path segments.
		{"https://homolog-api.vozkoia.com/instagram/oauth/callback", "path"},
		// http is not acceptable off localhost.
		{"http://homolog-api.vozkoia.com" + OAuthCallbackPath, "https"},
		// Relative URIs cannot be registered.
		{OAuthCallbackPath, "absolute"},
		// Query/fragment would break Meta's exact-string match.
		{"https://x.example" + OAuthCallbackPath + "?a=1", "query"},
		{"https://x.example" + OAuthCallbackPath + "#frag", "query"},
	}
	for _, c := range invalid {
		err := ValidateRedirectURI(c.uri)
		if err == nil {
			t.Errorf("ValidateRedirectURI(%q) = nil, want an error", c.uri)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("ValidateRedirectURI(%q) error = %q, want it to mention %q", c.uri, err, c.want)
		}
	}
}

// TestRedirectURIFor: a deployment configures a host, never the path.
func TestRedirectURIFor(t *testing.T) {
	cases := map[string]string{
		"https://api.example.com":     "https://api.example.com" + OAuthCallbackPath,
		"https://api.example.com/":    "https://api.example.com" + OAuthCallbackPath,
		"  https://api.example.com  ": "https://api.example.com" + OAuthCallbackPath,
	}
	for base, want := range cases {
		if got := RedirectURIFor(base); got != want {
			t.Errorf("RedirectURIFor(%q) = %q, want %q", base, got, want)
		}
		if err := ValidateRedirectURI(RedirectURIFor(base)); err != nil {
			t.Errorf("RedirectURIFor(%q) produced an invalid URI: %v", base, err)
		}
	}
}

// TestRequiredScopes documents that scopes are code, not configuration: they are a
// contract with the implementation and with what was submitted for App Review, so a
// deployment must not be able to silently drop one.
func TestRequiredScopes(t *testing.T) {
	scopes := RequiredScopes()

	// The short forms (business_basic, ...) were deprecated 2025-01-27.
	for _, s := range scopes {
		if !strings.HasPrefix(s, "instagram_business_") {
			t.Errorf("scope %q is not an instagram_business_* long form", s)
		}
	}
	// Messaging is the point of the channel; without it the connect flow hard-fails.
	var hasMessaging bool
	for _, s := range scopes {
		if s == ScopeManageMessages {
			hasMessaging = true
		}
	}
	if !hasMessaging {
		t.Errorf("RequiredScopes() is missing %s", ScopeManageMessages)
	}
}

// TestSubscribedFields_AreAllAcceptedByTheAPI is the regression test for a silent
// total failure: subscribed_fields validation is ATOMIC upstream, so one
// unrecognised entry rejects the whole call with code 100, subscribes nothing, and
// produces no webhooks at all, with silence as the only symptom.
//
// That is exactly what "message_echoes" did. The docs list it as subscribable; the
// API refuses it, and its rejection voided every other field in the same call.
func TestSubscribedFields_AreAllAcceptedByTheAPI(t *testing.T) {
	fields := SubscribedFields()

	if bad := InvalidSubscribedFields(fields); len(bad) > 0 {
		t.Fatalf("SubscribedFields() contains value(s) the API rejects: %v, a single bad "+
			"entry voids the entire subscription", bad)
	}

	set := map[string]bool{}
	for _, f := range fields {
		set[f] = true
	}

	// The fields the channel actually depends on.
	for _, want := range []string{
		"messages",          // inbound DMs, and echoes of our own sends
		"message_reactions", // reactions are a sibling event, not part of messages
		"message_edit",      // edited DMs
		"messaging_seen",    // read receipts (Instagram has no watermark)
		"comments",          // the moderation queue
	} {
		if !set[want] {
			t.Errorf("SubscribedFields() is missing %q", want)
		}
	}

	// message_echoes is documented but rejected: echoes ride on `messages` instead.
	if set["message_echoes"] {
		t.Error("SubscribedFields() contains message_echoes, which the API rejects with code 100")
	}
	// Instagram uses the SINGULAR forms; the Messenger reference uses plurals.
	for _, wrong := range []string{"messaging_referrals", "messaging_handovers", "messaging_reactions"} {
		if set[wrong] {
			t.Errorf("SubscribedFields() contains the Messenger plural %q", wrong)
		}
	}
	// message_reads is Messenger-only; Instagram uses messaging_seen.
	if set["message_reads"] {
		t.Error("SubscribedFields() contains message_reads, which does not exist for Instagram")
	}
}

// TestInvalidSubscribedFields covers the guard itself.
func TestInvalidSubscribedFields(t *testing.T) {
	if bad := InvalidSubscribedFields([]string{"messages", "comments"}); len(bad) != 0 {
		t.Errorf("valid fields reported as invalid: %v", bad)
	}

	bad := InvalidSubscribedFields([]string{"messages", "message_echoes", "nonsense"})
	if len(bad) != 2 {
		t.Fatalf("got %v, want 2 invalid entries", bad)
	}
	if bad[0] != "message_echoes" || bad[1] != "nonsense" {
		t.Errorf("got %v, want [message_echoes nonsense]", bad)
	}
}
