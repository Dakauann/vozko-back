package mercadolivre

import (
	"errors"
	"testing"
	"time"
)

func TestTokenGrantValidate(t *testing.T) {
	valid := func() *TokenGrant {
		return &TokenGrant{
			AccessToken:  "APP_USR-1",
			RefreshToken: "TG-1",
			ExpiresIn:    6 * time.Hour,
			UserID:       "1234567",
			Scopes:       []string{"offline_access", "read", "write"},
		}
	}
	cases := []struct {
		name   string
		mutate func(g *TokenGrant)
		err    error
	}{
		{"valid", func(g *TokenGrant) {}, nil},
		{"no access token", func(g *TokenGrant) { g.AccessToken = "" }, ErrCredentialsInvalid},
		{"no refresh token", func(g *TokenGrant) { g.RefreshToken = " " }, ErrCredentialsInvalid},
		{"no expiry", func(g *TokenGrant) { g.ExpiresIn = 0 }, ErrCredentialsInvalid},
		{"bad user", func(g *TokenGrant) { g.UserID = "me" }, ErrUserIDInvalid},
		{"read only grant", func(g *TokenGrant) { g.Scopes = []string{"offline_access", "read"} }, ErrScopeMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := valid()
			tc.mutate(g)
			err := g.Validate()
			if tc.err == nil && err != nil {
				t.Fatalf("Validate = %v", err)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("Validate = %v, want %v", err, tc.err)
			}
		})
	}
	var missing *TokenGrant
	if err := missing.Validate(); !errors.Is(err, ErrCredentialsInvalid) {
		t.Fatalf("nil grant = %v", err)
	}
}

func TestMessageStatus(t *testing.T) {
	cases := []struct {
		s            MessageStatus
		known, final bool
	}{
		{MessageAvailable, true, true},
		{MessagePendingReview, true, false},
		{MessageModerated, true, true},
		{MessageRejected, true, true},
		{MessagePendingTranslation, true, false},
		{MessageStatus("IN_MODERATION"), false, false},
		{MessageStatus(""), false, false},
	}
	for _, tc := range cases {
		if tc.s.Known() != tc.known || tc.s.Final() != tc.final {
			t.Errorf("%q known=%v final=%v", tc.s, tc.s.Known(), tc.s.Final())
		}
	}
}

func TestThreadStateComesFromTheThreadStatus(t *testing.T) {
	read := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	th := &Thread{Status: ThreadStatusBlocked, Substatus: "blocked_by_buyer"}
	s, err := th.State(ptrBool(false), read)
	if err != nil || s.WindowOpen || s.Detail != "blocked_by_buyer" {
		t.Fatalf("State = %+v, %v", s, err)
	}
}
