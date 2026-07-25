package shortlink_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vozko/domain/shortlink"
)

func TestResolve_EmptyCode(t *testing.T) {
	uc := NewResolveShortLinkUseCase(&fakeShortLinkRepo{}, newFakeSharedState())
	res, err := uc.Execute(context.Background(), "   ")
	if err != nil || res.State != shortlink.ResolveNotFound {
		t.Fatalf("empty code = %v %+v", err, res)
	}
}

func TestResolve_CacheHit(t *testing.T) {
	shared := newFakeSharedState()
	cached := shortlink.ResolvedLink{State: shortlink.ResolveOK, Code: "abc", TargetURL: "https://x.com"}
	payload, _ := json.Marshal(cached)
	shared.strs[resolveCacheKey("abc")] = string(payload)

	called := false
	repo := &fakeShortLinkRepo{FindByCodeFn: func(ctx context.Context, code string) (*shortlink.ShortLink, error) {
		called = true
		return nil, shortlink.ErrShortLinkNotFound
	}}
	uc := NewResolveShortLinkUseCase(repo, shared)

	res, err := uc.Execute(context.Background(), "abc")
	if err != nil || res.TargetURL != "https://x.com" {
		t.Fatalf("cache hit = %v %+v", err, res)
	}
	if called {
		t.Fatal("repo should not be called on cache hit")
	}
}

func TestResolve_CacheGetErrorAndInvalidJSON(t *testing.T) {
	repo := &fakeShortLinkRepo{FindByCodeFn: func(ctx context.Context, code string) (*shortlink.ShortLink, error) {
		return activeResolvableLink(), nil
	}}

	sharedErr := newFakeSharedState()
	sharedErr.getErr = errors.New("redis down")
	res, err := NewResolveShortLinkUseCase(repo, sharedErr).Execute(context.Background(), "abc")
	if err != nil || res.State != shortlink.ResolveOK {
		t.Fatalf("get error path = %v %+v", err, res)
	}

	sharedBad := newFakeSharedState()
	sharedBad.strs[resolveCacheKey("abc")] = "{not json"
	res, err = NewResolveShortLinkUseCase(repo, sharedBad).Execute(context.Background(), "abc")
	if err != nil || res.State != shortlink.ResolveOK {
		t.Fatalf("bad json path = %v %+v", err, res)
	}
}

func TestResolve_LoadStates(t *testing.T) {
	t.Run("not found negative cache", func(t *testing.T) {
		shared := newFakeSharedState()
		uc := NewResolveShortLinkUseCase(&fakeShortLinkRepo{}, shared)
		res, err := uc.Execute(context.Background(), "abc")
		if err != nil || res.State != shortlink.ResolveNotFound {
			t.Fatalf("not found = %v %+v", err, res)
		}
		if _, ok := shared.strs[resolveCacheKey("abc")]; !ok {
			t.Fatal("negative result should be cached")
		}
	})

	t.Run("repo error", func(t *testing.T) {
		repo := &fakeShortLinkRepo{FindByCodeFn: func(ctx context.Context, code string) (*shortlink.ShortLink, error) {
			return nil, errors.New("db")
		}}
		if _, err := NewResolveShortLinkUseCase(repo, newFakeSharedState()).Execute(context.Background(), "abc"); err == nil {
			t.Fatal("expected repo error")
		}
	})

	t.Run("ok nil shared", func(t *testing.T) {
		repo := &fakeShortLinkRepo{FindByCodeFn: func(ctx context.Context, code string) (*shortlink.ShortLink, error) {
			return activeResolvableLink(), nil
		}}
		res, err := NewResolveShortLinkUseCase(repo, nil).Execute(context.Background(), "abc")
		if err != nil || res.State != shortlink.ResolveOK {
			t.Fatalf("ok = %v %+v", err, res)
		}
	})

	t.Run("password", func(t *testing.T) {
		repo := &fakeShortLinkRepo{FindByCodeFn: func(ctx context.Context, code string) (*shortlink.ShortLink, error) {
			l := activeResolvableLink()
			l.PasswordHash = "h"
			return l, nil
		}}
		res, _ := NewResolveShortLinkUseCase(repo, newFakeSharedState()).Execute(context.Background(), "abc")
		if res.State != shortlink.ResolvePassword || !res.HasPassword {
			t.Fatalf("password state = %+v", res)
		}
	})

	t.Run("gone", func(t *testing.T) {
		repo := &fakeShortLinkRepo{FindByCodeFn: func(ctx context.Context, code string) (*shortlink.ShortLink, error) {
			l := activeResolvableLink()
			l.Status = shortlink.LinkStatusInactive
			return l, nil
		}}
		res, _ := NewResolveShortLinkUseCase(repo, newFakeSharedState()).Execute(context.Background(), "abc")
		if res.State != shortlink.ResolveGone {
			t.Fatalf("gone state = %+v", res)
		}
	})
}

func activeResolvableLink() *shortlink.ShortLink {
	return &shortlink.ShortLink{
		ID:           "id",
		WorkspaceID:  "ws",
		Code:         "abc",
		TargetURL:    "https://x.com",
		RedirectType: shortlink.RedirectTemporary,
		Status:       shortlink.LinkStatusActive,
		ExpiresAt:    func() *time.Time { t := time.Now().Add(time.Hour); return &t }(),
	}
}
