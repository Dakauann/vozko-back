package shortlink_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/shared"
	"vozko/domain/shortlink"
)

func TestGetShortLink(t *testing.T) {
	repo := &fakeShortLinkRepo{FindByIDFn: func(ctx context.Context, ws, id string) (*shortlink.ShortLink, error) {
		return &shortlink.ShortLink{ID: id}, nil
	}}
	uc := NewGetShortLinkUseCase(repo)
	link, err := uc.Execute(context.Background(), "ws", "id")
	if err != nil || link.ID != "id" {
		t.Fatalf("get failed: %v %+v", err, link)
	}
}

func TestListShortLinks(t *testing.T) {
	repo := &fakeShortLinkRepo{ListFn: func(ctx context.Context, ws string, dep *string, opts shared.Pagination) (*shared.PaginatedResult[*shortlink.ShortLink], error) {
		return shared.NewPaginatedResult([]*shortlink.ShortLink{{ID: "1"}}, opts, 1), nil
	}}
	uc := NewListShortLinksUseCase(repo)
	res, err := uc.Execute(context.Background(), "ws", nil, 1, 20)
	if err != nil || res.TotalItems != 1 {
		t.Fatalf("list failed: %v %+v", err, res)
	}
}

func TestDeleteShortLink(t *testing.T) {
	t.Run("success invalidates cache", func(t *testing.T) {
		shared := newFakeSharedState()
		shared.strs[resolveCacheKey("abc")] = "x"
		repo := &fakeShortLinkRepo{FindByIDFn: func(ctx context.Context, ws, id string) (*shortlink.ShortLink, error) {
			return &shortlink.ShortLink{ID: id, Code: "abc"}, nil
		}}
		uc := NewDeleteShortLinkUseCase(repo, shared)
		if err := uc.Execute(context.Background(), "ws", "id"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, ok := shared.strs[resolveCacheKey("abc")]; ok {
			t.Fatal("cache not invalidated")
		}
	})

	t.Run("nil shared", func(t *testing.T) {
		repo := &fakeShortLinkRepo{FindByIDFn: func(ctx context.Context, ws, id string) (*shortlink.ShortLink, error) {
			return &shortlink.ShortLink{ID: id, Code: "abc"}, nil
		}}
		uc := NewDeleteShortLinkUseCase(repo, nil)
		if err := uc.Execute(context.Background(), "ws", "id"); err != nil {
			t.Fatalf("delete: %v", err)
		}
	})

	t.Run("find error", func(t *testing.T) {
		repo := &fakeShortLinkRepo{FindByIDFn: func(ctx context.Context, ws, id string) (*shortlink.ShortLink, error) {
			return nil, shortlink.ErrShortLinkNotFound
		}}
		uc := NewDeleteShortLinkUseCase(repo, nil)
		if err := uc.Execute(context.Background(), "ws", "id"); err != shortlink.ErrShortLinkNotFound {
			t.Fatalf("expected not found, got %v", err)
		}
	})

	t.Run("delete error", func(t *testing.T) {
		repo := &fakeShortLinkRepo{
			FindByIDFn:   func(ctx context.Context, ws, id string) (*shortlink.ShortLink, error) { return &shortlink.ShortLink{Code: "abc"}, nil },
			SoftDeleteFn: func(ctx context.Context, ws, id string) error { return errors.New("db") },
		}
		uc := NewDeleteShortLinkUseCase(repo, nil)
		if err := uc.Execute(context.Background(), "ws", "id"); err == nil {
			t.Fatal("expected delete error")
		}
	})
}

func TestWorkspaceStats(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := &fakeShortLinkRepo{
			CountFn: func(ctx context.Context, ws string) (int, error) { return 4, nil },
			SumFn:   func(ctx context.Context, ws string) (int64, error) { return 99, nil },
		}
		uc := NewGetWorkspaceStatsUseCase(repo)
		stats, err := uc.Execute(context.Background(), "ws")
		if err != nil || stats.TotalLinks != 4 || stats.TotalClicks != 99 {
			t.Fatalf("stats wrong: %v %+v", err, stats)
		}
	})
	t.Run("count error", func(t *testing.T) {
		repo := &fakeShortLinkRepo{CountFn: func(ctx context.Context, ws string) (int, error) { return 0, errors.New("db") }}
		if _, err := NewGetWorkspaceStatsUseCase(repo).Execute(context.Background(), "ws"); err == nil {
			t.Fatal("expected count error")
		}
	})
	t.Run("sum error", func(t *testing.T) {
		repo := &fakeShortLinkRepo{SumFn: func(ctx context.Context, ws string) (int64, error) { return 0, errors.New("db") }}
		if _, err := NewGetWorkspaceStatsUseCase(repo).Execute(context.Background(), "ws"); err == nil {
			t.Fatal("expected sum error")
		}
	})
}
