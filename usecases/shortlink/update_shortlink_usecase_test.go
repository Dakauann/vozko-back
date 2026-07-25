package shortlink_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/shortlink"
)

func existingLink() *shortlink.ShortLink {
	return &shortlink.ShortLink{
		ID:           "id",
		WorkspaceID:  "ws",
		Code:         "abc123",
		TargetURL:    "https://old.com",
		RedirectType: shortlink.RedirectTemporary,
		Status:       shortlink.LinkStatusActive,
	}
}

func repoWithLink() *fakeShortLinkRepo {
	return &fakeShortLinkRepo{
		FindByIDFn: func(ctx context.Context, ws, id string) (*shortlink.ShortLink, error) {
			return existingLink(), nil
		},
	}
}

func strPtr(s string) *string { return &s }

func TestUpdate_FullSuccess(t *testing.T) {
	shared := newFakeSharedState()
	shared.strs[resolveCacheKey("abc123")] = "cached"
	repo := repoWithLink()
	uc := NewUpdateShortLinkUseCase(repo, fakeHostGuard{}, okScanner(), &fakePasswordSvc{}, shared, "")

	newURL := "https://new.com"
	title := "New"
	rt := "301"
	status := "inactive"
	pw := "secret"
	exp := time.Now().Add(time.Hour)
	max := int64(50)

	link, err := uc.Execute(context.Background(), "ws", "id", shortlink.UpdateShortLinkInput{
		TargetURL:    &newURL,
		Title:        &title,
		RedirectType: &rt,
		Status:       &status,
		Password:     &pw,
		ExpiresAt:    &exp,
		MaxClicks:    &max,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link.TargetURL != newURL || link.Title != "New" || link.PasswordHash == "" {
		t.Fatalf("fields not updated: %+v", link)
	}
	if link.RedirectType != shortlink.RedirectPermanent || link.Status != shortlink.LinkStatusInactive {
		t.Fatalf("enums not updated: %+v", link)
	}
	if _, ok := shared.strs[resolveCacheKey("abc123")]; ok {
		t.Fatal("cache should have been invalidated")
	}
}

func TestUpdate_ClearsAndNilShared(t *testing.T) {
	repo := &fakeShortLinkRepo{
		FindByIDFn: func(ctx context.Context, ws, id string) (*shortlink.ShortLink, error) {
			l := existingLink()
			l.PasswordHash = "old"
			exp := time.Now()
			l.ExpiresAt = &exp
			max := int64(10)
			l.MaxClicks = &max
			return l, nil
		},
	}
	uc := NewUpdateShortLinkUseCase(repo, fakeHostGuard{}, okScanner(), &fakePasswordSvc{}, nil, "")

	link, err := uc.Execute(context.Background(), "ws", "id", shortlink.UpdateShortLinkInput{
		ClearPassword:  true,
		ClearExpiry:    true,
		ClearMaxClicks: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link.PasswordHash != "" || link.ExpiresAt != nil || link.MaxClicks != nil {
		t.Fatalf("clears not applied: %+v", link)
	}
}

func TestUpdate_Errors(t *testing.T) {
	badURL := "ftp://x"
	loopURL := "https://example.com"
	newURL := "https://new.com"
	badRedirect := "308"
	badStatus := "paused"
	pw := "p"

	tests := []struct {
		name    string
		repo    *fakeShortLinkRepo
		guard   fakeHostGuard
		scanner fakeScanner
		pass    *fakePasswordSvc
		baseHost string
		input   shortlink.UpdateShortLinkInput
		wantErr error
	}{
		{
			name:    "not found",
			repo:    &fakeShortLinkRepo{FindByIDFn: func(ctx context.Context, ws, id string) (*shortlink.ShortLink, error) { return nil, shortlink.ErrShortLinkNotFound }},
			scanner: okScanner(),
			wantErr: shortlink.ErrShortLinkNotFound,
		},
		{
			name:    "bad target",
			repo:    repoWithLink(),
			scanner: okScanner(),
			input:   shortlink.UpdateShortLinkInput{TargetURL: &badURL},
			wantErr: shortlink.ErrTargetURLScheme,
		},
		{
			name:     "loop target",
			repo:     repoWithLink(),
			scanner:  okScanner(),
			baseHost: "example.com",
			input:    shortlink.UpdateShortLinkInput{TargetURL: &loopURL},
			wantErr:  shortlink.ErrTargetURLLoop,
		},
		{
			name:    "blocked target",
			repo:    repoWithLink(),
			guard:   fakeHostGuard{blocked: true},
			scanner: okScanner(),
			input:   shortlink.UpdateShortLinkInput{TargetURL: &newURL},
			wantErr: shortlink.ErrTargetURLBlocked,
		},
		{
			name:    "scanner error",
			repo:    repoWithLink(),
			scanner: fakeScanner{err: errors.New("scan")},
			input:   shortlink.UpdateShortLinkInput{TargetURL: &newURL},
			wantErr: errors.New("scan"),
		},
		{
			name:    "threat",
			repo:    repoWithLink(),
			scanner: fakeScanner{verdict: shortlink.ThreatVerdict{Safe: false}},
			input:   shortlink.UpdateShortLinkInput{TargetURL: &newURL},
			wantErr: shortlink.ErrThreatDetected,
		},
		{
			name:    "bad redirect",
			repo:    repoWithLink(),
			scanner: okScanner(),
			input:   shortlink.UpdateShortLinkInput{RedirectType: &badRedirect},
			wantErr: shortlink.ErrInvalidRedirectType,
		},
		{
			name:    "bad status",
			repo:    repoWithLink(),
			scanner: okScanner(),
			input:   shortlink.UpdateShortLinkInput{Status: &badStatus},
			wantErr: shortlink.ErrInvalidStatus,
		},
		{
			name:    "password hash error",
			repo:    repoWithLink(),
			scanner: okScanner(),
			pass:    &fakePasswordSvc{HashFn: func(plain string) (string, error) { return "", errors.New("bcrypt") }},
			input:   shortlink.UpdateShortLinkInput{Password: &pw},
			wantErr: errors.New("bcrypt"),
		},
		{
			name:    "update repo error",
			repo:    &fakeShortLinkRepo{FindByIDFn: func(ctx context.Context, ws, id string) (*shortlink.ShortLink, error) { return existingLink(), nil }, UpdateFn: func(ctx context.Context, l *shortlink.ShortLink) error { return errors.New("db") }},
			scanner: okScanner(),
			wantErr: errors.New("db"),
		},
		{
			name:    "title too long",
			repo:    repoWithLink(),
			scanner: okScanner(),
			input:   shortlink.UpdateShortLinkInput{Title: strPtr(strings.Repeat("a", shortlink.MaxTitleLength+1))},
			wantErr: shortlink.ErrTitleTooLong,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pass := tt.pass
			if pass == nil {
				pass = &fakePasswordSvc{}
			}
			uc := NewUpdateShortLinkUseCase(tt.repo, tt.guard, tt.scanner, pass, newFakeSharedState(), tt.baseHost)
			_, err := uc.Execute(context.Background(), "ws", "id", tt.input)
			assertErr(t, err, tt.wantErr)
		})
	}
}
