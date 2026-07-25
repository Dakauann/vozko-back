package shortlink_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/shortlink"
)

func TestGenerateQR(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var content string
		repo := &fakeShortLinkRepo{FindByIDFn: func(ctx context.Context, ws, id string) (*shortlink.ShortLink, error) {
			return &shortlink.ShortLink{ID: id, Code: "abc"}, nil
		}}
		uc := NewGenerateQRUseCase(repo, qrCapturing{content: &content, data: []byte{0x89, 'P', 'N', 'G'}}, "https://vx.co/r")
		png, err := uc.Execute(context.Background(), "ws", "id", 200)
		if err != nil || len(png) == 0 {
			t.Fatalf("qr = %v %v", err, png)
		}
		if content != "https://vx.co/r/abc" {
			t.Fatalf("qr content = %q", content)
		}
	})

	t.Run("not found", func(t *testing.T) {
		repo := &fakeShortLinkRepo{FindByIDFn: func(ctx context.Context, ws, id string) (*shortlink.ShortLink, error) {
			return nil, shortlink.ErrShortLinkNotFound
		}}
		uc := NewGenerateQRUseCase(repo, fakeQR{}, "https://vx.co/r")
		if _, err := uc.Execute(context.Background(), "ws", "id", 200); err != shortlink.ErrShortLinkNotFound {
			t.Fatalf("got %v", err)
		}
	})
}

type qrCapturing struct {
	content *string
	data    []byte
	err     error
}

func (q qrCapturing) Generate(content string, size int) ([]byte, error) {
	*q.content = content
	return q.data, q.err
}

func TestClampQRSize(t *testing.T) {
	cases := map[int]int{
		0:             defaultQRSize,
		-5:            defaultQRSize,
		minQRSize - 1: minQRSize,
		maxQRSize + 1: maxQRSize,
		300:           300,
	}
	for in, want := range cases {
		if got := clampQRSize(in); got != want {
			t.Fatalf("clampQRSize(%d) = %d want %d", in, got, want)
		}
	}
}

func TestPurgeClicks(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		called := false
		repo := &fakeClickRepo{PurgeClicksFn: func(ctx context.Context, cutoff time.Time) (int64, error) {
			called = true
			return 0, nil
		}}
		if err := NewPurgeClicksUseCase(repo, 0).Execute(context.Background()); err != nil {
			t.Fatalf("disabled: %v", err)
		}
		if called {
			t.Fatal("purge should not run when retention <= 0")
		}
	})

	t.Run("success", func(t *testing.T) {
		repo := &fakeClickRepo{
			PurgeClicksFn: func(ctx context.Context, cutoff time.Time) (int64, error) { return 3, nil },
			PurgeDailyFn:  func(ctx context.Context, cutoff time.Time) (int64, error) { return 2, nil },
		}
		if err := NewPurgeClicksUseCase(repo, 30).Execute(context.Background()); err != nil {
			t.Fatalf("success: %v", err)
		}
	})

	t.Run("clicks error", func(t *testing.T) {
		repo := &fakeClickRepo{PurgeClicksFn: func(ctx context.Context, cutoff time.Time) (int64, error) { return 0, errors.New("db") }}
		if err := NewPurgeClicksUseCase(repo, 30).Execute(context.Background()); err == nil {
			t.Fatal("expected clicks purge error")
		}
	})

	t.Run("daily error", func(t *testing.T) {
		repo := &fakeClickRepo{PurgeDailyFn: func(ctx context.Context, cutoff time.Time) (int64, error) { return 0, errors.New("db") }}
		if err := NewPurgeClicksUseCase(repo, 30).Execute(context.Background()); err == nil {
			t.Fatal("expected daily purge error")
		}
	})
}

func TestHelpers(t *testing.T) {
	if HashIP("salt", "") != "" {
		t.Fatal("empty ip must hash to empty")
	}
	h1 := HashIP("salt", "1.2.3.4")
	h2 := HashIP("salt", "1.2.3.4")
	if h1 == "" || h1 != h2 {
		t.Fatal("hash must be deterministic and non-empty")
	}
	if HashIP("other", "1.2.3.4") == h1 {
		t.Fatal("different salt must change hash")
	}

	if RefererDomain("") != "" {
		t.Fatal("empty referer")
	}
	if RefererDomain("https://Ref.com/page") != "ref.com" {
		t.Fatalf("referer host = %q", RefererDomain("https://Ref.com/page"))
	}
	if RefererDomain("http://\x7f") != "" {
		t.Fatal("unparseable referer must be empty")
	}

	if rt, err := resolveRedirectType(""); err != nil || rt != shortlink.RedirectTemporary {
		t.Fatalf("empty redirect = %v %v", rt, err)
	}
	if rt, err := resolveRedirectType("301"); err != nil || rt != shortlink.RedirectPermanent {
		t.Fatalf("301 = %v %v", rt, err)
	}
	if _, err := resolveRedirectType("308"); err != shortlink.ErrInvalidRedirectType {
		t.Fatalf("invalid redirect err = %v", err)
	}

	if resolveCacheKey("abc") != "shortlink:resolve:abc" {
		t.Fatal("cache key format")
	}
	if uniqueVisitorKey("l", "h") != "shortlink:uniq:l:h" {
		t.Fatal("unique key format")
	}
}
