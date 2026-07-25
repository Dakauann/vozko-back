package shortlink_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/shortlink"
)

func okScanner() fakeScanner { return fakeScanner{verdict: shortlink.ThreatVerdict{Safe: true}} }

func TestCreate_Success_RandomCode(t *testing.T) {
	repo := &fakeShortLinkRepo{
		CountFn:      func(ctx context.Context, ws string) (int, error) { return 0, nil },
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) { return false, nil },
	}
	uc := NewCreateShortLinkUseCase(repo, fakeHostGuard{}, okScanner(), &fakePasswordSvc{}, 7, "vx.co")

	link, err := uc.Execute(context.Background(), shortlink.CreateShortLinkInput{
		WorkspaceID: "ws",
		CreatedBy:   "user",
		TargetURL:   "https://example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link.Code == "" || link.ID == "" {
		t.Fatalf("code/id not set: %+v", link)
	}
	if link.Status != shortlink.LinkStatusActive || link.RedirectType != shortlink.RedirectTemporary {
		t.Fatalf("defaults wrong: %+v", link)
	}
}

func TestCreate_Success_CustomAliasPasswordDepartment(t *testing.T) {
	repo := &fakeShortLinkRepo{
		CountFn:      func(ctx context.Context, ws string) (int, error) { return 3, nil },
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) { return false, nil },
	}
	dep := "dep-1"
	uc := NewCreateShortLinkUseCase(repo, fakeHostGuard{}, okScanner(), &fakePasswordSvc{}, 7, "")

	link, err := uc.Execute(context.Background(), shortlink.CreateShortLinkInput{
		WorkspaceID:  "ws",
		DepartmentID: &dep,
		TargetURL:    "https://example.com",
		CustomAlias:  "promo-julho",
		Password:     "secret",
		RedirectType: "301",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link.Code != "promo-julho" || link.PasswordHash == "" || link.DepartmentID != "dep-1" {
		t.Fatalf("fields wrong: %+v", link)
	}
	if link.RedirectType != shortlink.RedirectPermanent {
		t.Fatalf("redirect = %q", link.RedirectType)
	}
}

func TestCreate_Errors(t *testing.T) {
	base := shortlink.CreateShortLinkInput{WorkspaceID: "ws", TargetURL: "https://example.com"}

	tests := []struct {
		name    string
		repo    *fakeShortLinkRepo
		guard   fakeHostGuard
		scanner fakeScanner
		pass    *fakePasswordSvc
		baseHost string
		input   shortlink.CreateShortLinkInput
		wantErr error
	}{
		{
			name:    "missing workspace",
			repo:    &fakeShortLinkRepo{},
			scanner: okScanner(),
			input:   shortlink.CreateShortLinkInput{TargetURL: "https://x.com"},
			wantErr: shortlink.ErrWorkspaceIDRequired,
		},
		{
			name:    "invalid target",
			repo:    &fakeShortLinkRepo{},
			scanner: okScanner(),
			input:   shortlink.CreateShortLinkInput{WorkspaceID: "ws", TargetURL: "ftp://x"},
			wantErr: shortlink.ErrTargetURLScheme,
		},
		{
			name:     "loop",
			repo:     &fakeShortLinkRepo{},
			scanner:  okScanner(),
			baseHost: "example.com",
			input:    base,
			wantErr:  shortlink.ErrTargetURLLoop,
		},
		{
			name:    "blocked host",
			repo:    &fakeShortLinkRepo{},
			guard:   fakeHostGuard{blocked: true},
			scanner: okScanner(),
			input:   base,
			wantErr: shortlink.ErrTargetURLBlocked,
		},
		{
			name:    "scanner error",
			repo:    &fakeShortLinkRepo{},
			scanner: fakeScanner{err: errors.New("scan down")},
			input:   base,
			wantErr: errors.New("scan down"),
		},
		{
			name:    "threat detected",
			repo:    &fakeShortLinkRepo{},
			scanner: fakeScanner{verdict: shortlink.ThreatVerdict{Safe: false}},
			input:   base,
			wantErr: shortlink.ErrThreatDetected,
		},
		{
			name:    "invalid redirect",
			repo:    &fakeShortLinkRepo{},
			scanner: okScanner(),
			input:   shortlink.CreateShortLinkInput{WorkspaceID: "ws", TargetURL: "https://x.com", RedirectType: "308"},
			wantErr: shortlink.ErrInvalidRedirectType,
		},
		{
			name:    "count error",
			repo:    &fakeShortLinkRepo{CountFn: func(ctx context.Context, ws string) (int, error) { return 0, errors.New("db") }},
			scanner: okScanner(),
			input:   base,
			wantErr: errors.New("db"),
		},
		{
			name:    "max reached",
			repo:    &fakeShortLinkRepo{CountFn: func(ctx context.Context, ws string) (int, error) { return shortlink.MaxShortLinksPerWorkspace, nil }},
			scanner: okScanner(),
			input:   base,
			wantErr: shortlink.ErrMaxShortLinksReached,
		},
		{
			name:    "alias invalid",
			repo:    &fakeShortLinkRepo{},
			scanner: okScanner(),
			input:   shortlink.CreateShortLinkInput{WorkspaceID: "ws", TargetURL: "https://x.com", CustomAlias: "no"},
			wantErr: shortlink.ErrInvalidAliasLength,
		},
		{
			name:    "alias exists error",
			repo:    &fakeShortLinkRepo{CodeExistsFn: func(ctx context.Context, code string) (bool, error) { return false, errors.New("db") }},
			scanner: okScanner(),
			input:   shortlink.CreateShortLinkInput{WorkspaceID: "ws", TargetURL: "https://x.com", CustomAlias: "promo-julho"},
			wantErr: errors.New("db"),
		},
		{
			name:    "alias taken",
			repo:    &fakeShortLinkRepo{CodeExistsFn: func(ctx context.Context, code string) (bool, error) { return true, nil }},
			scanner: okScanner(),
			input:   shortlink.CreateShortLinkInput{WorkspaceID: "ws", TargetURL: "https://x.com", CustomAlias: "promo-julho"},
			wantErr: shortlink.ErrCodeTaken,
		},
		{
			name:    "random codeexists error",
			repo:    &fakeShortLinkRepo{CodeExistsFn: func(ctx context.Context, code string) (bool, error) { return false, errors.New("db") }},
			scanner: okScanner(),
			input:   base,
			wantErr: errors.New("db"),
		},
		{
			name:    "random exhausted",
			repo:    &fakeShortLinkRepo{CodeExistsFn: func(ctx context.Context, code string) (bool, error) { return true, nil }},
			scanner: okScanner(),
			input:   base,
			wantErr: shortlink.ErrCodeGenerationFailed,
		},
		{
			name:    "create error",
			repo:    &fakeShortLinkRepo{CreateFn: func(ctx context.Context, l *shortlink.ShortLink) error { return errors.New("insert") }},
			scanner: okScanner(),
			input:   base,
			wantErr: errors.New("insert"),
		},
		{
			name:    "title too long",
			repo:    &fakeShortLinkRepo{},
			scanner: okScanner(),
			input:   shortlink.CreateShortLinkInput{WorkspaceID: "ws", TargetURL: "https://x.com", Title: strings.Repeat("a", shortlink.MaxTitleLength+1)},
			wantErr: shortlink.ErrTitleTooLong,
		},
		{
			name:    "password hash error",
			repo:    &fakeShortLinkRepo{},
			scanner: okScanner(),
			pass:    &fakePasswordSvc{HashFn: func(plain string) (string, error) { return "", errors.New("bcrypt") }},
			input:   shortlink.CreateShortLinkInput{WorkspaceID: "ws", TargetURL: "https://x.com", Password: "p"},
			wantErr: errors.New("bcrypt"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pass := tt.pass
			if pass == nil {
				pass = &fakePasswordSvc{}
			}
			uc := NewCreateShortLinkUseCase(tt.repo, tt.guard, tt.scanner, pass, 7, tt.baseHost)
			_, err := uc.Execute(context.Background(), tt.input)
			assertErr(t, err, tt.wantErr)
		})
	}
}

func TestCreate_GenerateCodeError(t *testing.T) {
	orig := generateCode
	generateCode = func(int) (string, error) { return "", errors.New("rand") }
	defer func() { generateCode = orig }()

	uc := NewCreateShortLinkUseCase(&fakeShortLinkRepo{}, fakeHostGuard{}, okScanner(), &fakePasswordSvc{}, 7, "")
	_, err := uc.Execute(context.Background(), shortlink.CreateShortLinkInput{WorkspaceID: "ws", TargetURL: "https://x.com"})
	if err == nil || err.Error() != "rand" {
		t.Fatalf("expected rand error, got %v", err)
	}
}

func assertErr(t *testing.T, got, want error) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("expected nil error, got %v", got)
		}
		return
	}
	if got == nil {
		t.Fatalf("expected error %v, got nil", want)
	}
	if !errors.Is(got, want) && got.Error() != want.Error() {
		t.Fatalf("error = %v want %v", got, want)
	}
}
