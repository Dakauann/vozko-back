package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const modelsListBody = `{
  "data": [
    {
      "id": "anthropic/claude-sonnet-4.5",
      "name": "Claude Sonnet 4.5",
      "created": 1750000000,
      "context_length": 200000,
      "pricing": { "prompt": "0.000003", "completion": "0.000015" }
    },
    {
      "id": "google/gemini-3-flash",
      "name": "Gemini 3 Flash",
      "created": 1751000000,
      "pricing": { "prompt": "0.0000001", "completion": "0.0000004" },
      "top_provider": { "context_length": 1000000 }
    }
  ]
}`

func TestModelCatalogFetcher_SortAndParse(t *testing.T) {
	var gotQuery atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery.Store(r.URL.RawQuery)
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q, want bearer test-key", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(modelsListBody))
	}))
	defer srv.Close()

	f := newModelCatalogFetcher("test-key", srv.URL)
	models, ok := f.FetchModelsWithPricing(context.Background())
	if !ok {
		t.Fatal("FetchModelsWithPricing ok = false, want true")
	}

	if q, _ := gotQuery.Load().(string); !strings.Contains(q, "sort=most-popular") ||
		!strings.Contains(q, "output_modalities=text") ||
		!strings.Contains(q, "supported_parameters=tools") {
		t.Errorf("query = %q, want sort=most-popular & output_modalities=text & supported_parameters=tools", q)
	}

	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}

	// Order preserved (server returns most-popular order).
	if models[0].ID != "anthropic/claude-sonnet-4.5" {
		t.Errorf("models[0].ID = %q, want anthropic/claude-sonnet-4.5", models[0].ID)
	}

	// Prices scaled to per-million tokens.
	if got := models[0].PromptPrice; got != 3.0 {
		t.Errorf("models[0].PromptPrice = %v, want 3", got)
	}
	if got := models[0].CompletionPrice; got != 15.0 {
		t.Errorf("models[0].CompletionPrice = %v, want 15", got)
	}

	// Top-level context_length.
	if got := models[0].ContextLength; got != 200000 {
		t.Errorf("models[0].ContextLength = %d, want 200000", got)
	}
	if models[0].Created != 1750000000 {
		t.Errorf("models[0].Created = %d, want 1750000000", models[0].Created)
	}

	// Falls back to top_provider.context_length when top-level is absent.
	if got := models[1].ContextLength; got != 1000000 {
		t.Errorf("models[1].ContextLength = %d, want 1000000 (top_provider fallback)", got)
	}
}

func TestModelCatalogFetcher_CachesWithinTTL(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(modelsListBody))
	}))
	defer srv.Close()

	base := time.Unix(1_700_000_000, 0)
	f := newModelCatalogFetcher("test-key", srv.URL)
	f.now = func() time.Time { return base }

	if _, ok := f.FetchModelsWithPricing(context.Background()); !ok {
		t.Fatal("first fetch ok = false")
	}
	// Within TTL: served from cache, no extra HTTP call.
	f.now = func() time.Time { return base.Add(modelCatalogTTL - time.Minute) }
	if _, ok := f.FetchModelsWithPricing(context.Background()); !ok {
		t.Fatal("cached fetch ok = false")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("HTTP calls = %d within TTL, want 1", got)
	}

	// Past TTL: refetches.
	f.now = func() time.Time { return base.Add(modelCatalogTTL + time.Minute) }
	if _, ok := f.FetchModelsWithPricing(context.Background()); !ok {
		t.Fatal("refetch ok = false")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("HTTP calls = %d past TTL, want 2", got)
	}
}

func TestModelCatalogFetcher_NoKeyOrError(t *testing.T) {
	// No API key → ok=false so the caller falls back to the library path.
	f := newModelCatalogFetcher("", openRouterDefaultBaseURL)
	if _, ok := f.FetchModelsWithPricing(context.Background()); ok {
		t.Error("ok = true with empty api key, want false")
	}

	// Non-200 → ok=false.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	f2 := newModelCatalogFetcher("k", srv.URL)
	if _, ok := f2.FetchModelsWithPricing(context.Background()); ok {
		t.Error("ok = true on 500, want false")
	}
}
