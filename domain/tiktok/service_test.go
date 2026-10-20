package tiktok

import (
	"errors"
	"testing"
	"time"
)

func TestGrantValidateRefusesIncompleteGrants(t *testing.T) {
	valid := Grant{OpenID: "_000abc", AccessToken: "a", RefreshToken: "r", ExpiresIn: time.Hour}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid grant: %v", err)
	}
	broken := map[string]Grant{
		"no open id":       {AccessToken: "a", RefreshToken: "r", ExpiresIn: time.Hour},
		"bad open id":      {OpenID: "a b", AccessToken: "a", RefreshToken: "r", ExpiresIn: time.Hour},
		"no access token":  {OpenID: "_000abc", RefreshToken: "r", ExpiresIn: time.Hour},
		"no refresh token": {OpenID: "_000abc", AccessToken: "a", ExpiresIn: time.Hour},
		"no lifetime":      {OpenID: "_000abc", AccessToken: "a", RefreshToken: "r"},
	}
	for name, g := range broken {
		if err := g.Validate(); !errors.Is(err, ErrGrantIncomplete) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestWebhookKindsCoverMessagesAndComments(t *testing.T) {
	got := WebhookKinds()
	if len(got) != 2 || got[0] != WebhookDirectMessage || got[1] != WebhookComment {
		t.Fatalf("WebhookKinds = %v", got)
	}
}
