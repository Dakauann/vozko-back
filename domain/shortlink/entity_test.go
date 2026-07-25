package shortlink

import (
	"testing"
	"time"
)

func ptrInt64(v int64) *int64 { return &v }

func TestLinkStatusIsValid(t *testing.T) {
	cases := map[LinkStatus]bool{
		LinkStatusActive:    true,
		LinkStatusInactive:  true,
		LinkStatus("weird"): false,
		LinkStatus(""):      false,
	}
	for status, want := range cases {
		if got := status.IsValid(); got != want {
			t.Fatalf("status %q IsValid()=%v want %v", status, got, want)
		}
	}
}

func TestRedirectTypeIsValidAndStatus(t *testing.T) {
	if !RedirectTemporary.IsValid() || !RedirectPermanent.IsValid() {
		t.Fatal("302 and 301 must be valid")
	}
	if RedirectType("307").IsValid() {
		t.Fatal("307 must be invalid")
	}
	if RedirectTemporary.HTTPStatus() != 302 {
		t.Fatalf("temporary status = %d", RedirectTemporary.HTTPStatus())
	}
	if RedirectPermanent.HTTPStatus() != 301 {
		t.Fatalf("permanent status = %d", RedirectPermanent.HTTPStatus())
	}
}

func TestNormalizeDefaultsAndTrims(t *testing.T) {
	link := &ShortLink{
		ID:           "  id  ",
		WorkspaceID:  " ws ",
		DepartmentID: " dep ",
		Code:         "  abc  ",
		TargetURL:    "  https://x.com  ",
		Title:        "  hi  ",
		PasswordHash: "hash",
	}
	link.Normalize()

	if link.Code != "abc" || link.TargetURL != "https://x.com" || link.Title != "hi" {
		t.Fatalf("fields not trimmed: %+v", link)
	}
	if link.WorkspaceID != "ws" || link.DepartmentID != "dep" || link.ID != "id" {
		t.Fatalf("ids not trimmed: %+v", link)
	}
	if link.Status != LinkStatusActive {
		t.Fatalf("default status = %q", link.Status)
	}
	if link.RedirectType != RedirectTemporary {
		t.Fatalf("default redirect = %q", link.RedirectType)
	}
	if !link.HasPassword {
		t.Fatal("HasPassword should be true when hash present")
	}
}

func validLink() *ShortLink {
	return &ShortLink{
		ID:           "id",
		WorkspaceID:  "ws",
		Code:         "abc123",
		TargetURL:    "https://example.com",
		RedirectType: RedirectTemporary,
		Status:       LinkStatusActive,
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(l *ShortLink)
		wantErr error
	}{
		{"valid", func(l *ShortLink) {}, nil},
		{"missing id", func(l *ShortLink) { l.ID = "" }, ErrShortLinkIDRequired},
		{"missing workspace", func(l *ShortLink) { l.WorkspaceID = "" }, ErrWorkspaceIDRequired},
		{"missing code", func(l *ShortLink) { l.Code = "" }, ErrCodeRequired},
		{"bad redirect", func(l *ShortLink) { l.RedirectType = "308" }, ErrInvalidRedirectType},
		{"bad status", func(l *ShortLink) { l.Status = "paused" }, ErrInvalidStatus},
		{"title too long", func(l *ShortLink) { l.Title = longString(MaxTitleLength + 1) }, ErrTitleTooLong},
		{"bad max clicks", func(l *ShortLink) { l.MaxClicks = ptrInt64(0) }, ErrInvalidMaxClicks},
		{"bad target", func(l *ShortLink) { l.TargetURL = "ftp://x" }, ErrTargetURLScheme},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := validLink()
			tt.mutate(link)
			err := link.Validate()
			if err != tt.wantErr {
				t.Fatalf("Validate() = %v want %v", err, tt.wantErr)
			}
		})
	}
}

func TestExpiryAndLimits(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	link := validLink()
	if link.IsExpired(now) {
		t.Fatal("nil expiry must not be expired")
	}
	link.ExpiresAt = &past
	if !link.IsExpired(now) {
		t.Fatal("past expiry must be expired")
	}
	link.ExpiresAt = &now
	if !link.IsExpired(now) {
		t.Fatal("expiry exactly now must be expired")
	}
	link.ExpiresAt = &future
	if link.IsExpired(now) {
		t.Fatal("future expiry must not be expired")
	}

	link.MaxClicks = nil
	if link.ReachedClickLimit() {
		t.Fatal("nil max clicks never reaches limit")
	}
	link.MaxClicks = ptrInt64(5)
	link.ClickCount = 4
	if link.ReachedClickLimit() {
		t.Fatal("4/5 not reached")
	}
	link.ClickCount = 5
	if !link.ReachedClickLimit() {
		t.Fatal("5/5 reached")
	}
}

func TestIsResolvableAndPassword(t *testing.T) {
	now := time.Now()
	link := validLink()
	if !link.IsResolvable(now) {
		t.Fatal("active link should resolve")
	}
	if link.HasPasswordProtection() {
		t.Fatal("no password by default")
	}
	link.PasswordHash = "h"
	if !link.HasPasswordProtection() {
		t.Fatal("password should be detected")
	}

	inactive := validLink()
	inactive.Status = LinkStatusInactive
	if inactive.IsResolvable(now) {
		t.Fatal("inactive must not resolve")
	}

	expired := validLink()
	past := now.Add(-time.Hour)
	expired.ExpiresAt = &past
	if expired.IsResolvable(now) {
		t.Fatal("expired must not resolve")
	}

	capped := validLink()
	capped.MaxClicks = ptrInt64(1)
	capped.ClickCount = 1
	if capped.IsResolvable(now) {
		t.Fatal("capped must not resolve")
	}
}

func longString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
