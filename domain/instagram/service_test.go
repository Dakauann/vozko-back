package instagram

import (
	"strings"
	"testing"
)

func TestValidateRedirectURI(t *testing.T) {
	valid := []string{
		"https://homolog-api.vozkoia.com" + OAuthCallbackPath,
		"https://homolog-api.vozkoia.com" + OAuthCallbackPath + "/",
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
		{"https://homolog-api.vozkoia.com/webhooks/instagram", "path"},
		{"https://homolog-api.vozkoia.com/instagram/oauth/callback", "path"},
		{"http://homolog-api.vozkoia.com" + OAuthCallbackPath, "https"},
		{OAuthCallbackPath, "absolute"},
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

func TestRequiredScopes(t *testing.T) {
	scopes := RequiredScopes()

	for _, s := range scopes {
		if !strings.HasPrefix(s, "instagram_business_") {
			t.Errorf("scope %q is not an instagram_business_* long form", s)
		}
	}
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

	for _, want := range []string{
		"messages",
		"message_reactions",
		"message_edit",
		"messaging_seen",
		"comments",
	} {
		if !set[want] {
			t.Errorf("SubscribedFields() is missing %q", want)
		}
	}

	if set["message_echoes"] {
		t.Error("SubscribedFields() contains message_echoes, which the API rejects with code 100")
	}
	for _, wrong := range []string{"messaging_referrals", "messaging_handovers", "messaging_reactions"} {
		if set[wrong] {
			t.Errorf("SubscribedFields() contains the Messenger plural %q", wrong)
		}
	}
	if set["message_reads"] {
		t.Error("SubscribedFields() contains message_reads, which does not exist for Instagram")
	}
}

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
