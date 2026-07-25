package shortlink_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/shortlink"
)

func TestUnlock(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		repo := &fakeShortLinkRepo{FindByCodeFn: func(ctx context.Context, code string) (*shortlink.ShortLink, error) {
			return nil, shortlink.ErrShortLinkNotFound
		}}
		if _, err := NewUnlockShortLinkUseCase(repo, &fakePasswordSvc{}).Execute(context.Background(), "abc", "p"); err != shortlink.ErrShortLinkNotFound {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("gone", func(t *testing.T) {
		repo := &fakeShortLinkRepo{FindByCodeFn: func(ctx context.Context, code string) (*shortlink.ShortLink, error) {
			l := activeResolvableLink()
			l.Status = shortlink.LinkStatusInactive
			return l, nil
		}}
		if _, err := NewUnlockShortLinkUseCase(repo, &fakePasswordSvc{}).Execute(context.Background(), "abc", "p"); err != shortlink.ErrShortLinkNotFound {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("password required", func(t *testing.T) {
		repo := passwordLinkRepo()
		if _, err := NewUnlockShortLinkUseCase(repo, &fakePasswordSvc{}).Execute(context.Background(), "abc", "  "); err != shortlink.ErrPasswordRequired {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		repo := passwordLinkRepo()
		pass := &fakePasswordSvc{VerifyFn: func(hash, plain string) error { return errors.New("mismatch") }}
		if _, err := NewUnlockShortLinkUseCase(repo, pass).Execute(context.Background(), "abc", "wrong"); err != shortlink.ErrInvalidPassword {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("correct password", func(t *testing.T) {
		repo := passwordLinkRepo()
		res, err := NewUnlockShortLinkUseCase(repo, &fakePasswordSvc{}).Execute(context.Background(), "abc", "right")
		if err != nil || res.State != shortlink.ResolveOK || res.TargetURL != "https://x.com" {
			t.Fatalf("unlock = %v %+v", err, res)
		}
	})

	t.Run("no password link", func(t *testing.T) {
		repo := &fakeShortLinkRepo{FindByCodeFn: func(ctx context.Context, code string) (*shortlink.ShortLink, error) {
			return activeResolvableLink(), nil
		}}
		res, err := NewUnlockShortLinkUseCase(repo, &fakePasswordSvc{}).Execute(context.Background(), "abc", "")
		if err != nil || res.State != shortlink.ResolveOK {
			t.Fatalf("no password = %v %+v", err, res)
		}
	})
}

func passwordLinkRepo() *fakeShortLinkRepo {
	return &fakeShortLinkRepo{FindByCodeFn: func(ctx context.Context, code string) (*shortlink.ShortLink, error) {
		l := activeResolvableLink()
		l.PasswordHash = "h"
		return l, nil
	}}
}
