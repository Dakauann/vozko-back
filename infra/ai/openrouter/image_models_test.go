package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

const imageModelsBody = `{
  "data": [
    {"id": "openai/gpt-image-2.5-sunburst", "name": "GPT Image 2.5 Sunburst", "architecture": {"output_modalities": ["image"]}},
    {"id": "google/gemini-3-pro-image", "name": "Gemini 3 Pro Image", "architecture": {"output_modalities": ["image", "text"]}}
  ]
}`

func TestImageModelsAreRankedByPopularity(t *testing.T) {
	var gotQuery atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery.Store(r.URL.Query())
		_, _ = w.Write([]byte(imageModelsBody))
	}))
	defer srv.Close()

	models, err := newImageModelCatalog("test-key", srv.URL).ImageModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	q, _ := gotQuery.Load().(url.Values)
	if q.Get("sort") != "most-popular" || q.Get("output_modalities") != "image" || q.Has("supported_parameters") {
		t.Fatalf("query %v", q)
	}
	if len(models) != 2 || models[0].ID != "openai/gpt-image-2.5-sunburst" || models[1].Name != "Gemini 3 Pro Image" {
		t.Fatalf("models %+v", models)
	}
}

func TestAnUnavailableImageCatalogIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	if _, err := newImageModelCatalog("test-key", srv.URL).ImageModels(context.Background()); err == nil {
		t.Fatal("a failed fetch returned no error")
	}
	if _, err := newImageModelCatalog("", srv.URL).ImageModels(context.Background()); err == nil {
		t.Fatal("a missing key returned no error")
	}
}
