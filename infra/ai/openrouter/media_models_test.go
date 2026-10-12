package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"vozko/domain/mediagen"
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

	models, err := newMediaModelCatalog("test-key", srv.URL).Models(context.Background(), mediagen.KindImage)
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

	if _, err := newMediaModelCatalog("test-key", srv.URL).Models(context.Background(), mediagen.KindImage); err == nil {
		t.Fatal("a failed fetch returned no error")
	}
	if _, err := newMediaModelCatalog("", srv.URL).Models(context.Background(), mediagen.KindImage); err == nil {
		t.Fatal("a missing key returned no error")
	}
}

const audioModelsBody = `{
  "data": [
    {"id": "google/lyria-3-clip-preview", "name": "Lyria 3 Clip", "architecture": {"input_modalities": ["text", "image"], "output_modalities": ["text", "audio"]}},
    {"id": "openai/gpt-audio-mini", "name": "GPT Audio Mini", "architecture": {"input_modalities": ["text", "audio"], "output_modalities": ["text", "audio"]}}
  ]
}`

func TestAudioModelsAreSplitIntoMusicAndVoiceByWhetherTheyHear(t *testing.T) {
	var queries []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query())
		_, _ = w.Write([]byte(audioModelsBody))
	}))
	defer srv.Close()
	catalog := newMediaModelCatalog("test-key", srv.URL)
	music, err := catalog.Models(context.Background(), mediagen.KindMusic)
	if err != nil || len(music) != 1 || music[0].ID != "google/lyria-3-clip-preview" {
		t.Fatalf("music %+v err %v", music, err)
	}
	voice, err := catalog.Models(context.Background(), mediagen.KindVoice)
	if err != nil || len(voice) != 1 || voice[0].ID != "openai/gpt-audio-mini" {
		t.Fatalf("voice %+v err %v", voice, err)
	}
	if len(queries) != 1 || queries[0].Get("output_modalities") != "audio" {
		t.Fatalf("the audio catalog is fetched once and cached, queries %v", queries)
	}
	if _, err := catalog.Models(context.Background(), mediagen.KindVideo); err == nil {
		t.Fatal("a render has no model catalog")
	}
}

const speechModelsBody = `{
  "data": [
    {"id": "elevenlabs/eleven-v4", "name": "Eleven v4", "pricing": {"prompt": "0.00004", "completion": "0"}, "architecture": {"input_modalities": ["text"], "output_modalities": ["speech"]}},
    {"id": "bytedance-seed/seed-audio-1-0", "name": "Seed Audio 1.0", "pricing": {"prompt": "0", "completion": "0.0025"}, "architecture": {"input_modalities": ["text"], "output_modalities": ["speech"]}},
    {"id": "fish-audio/s2.1-pro-free:free", "name": "Free", "pricing": {"prompt": "0", "completion": "0"}, "architecture": {"input_modalities": ["text"], "output_modalities": ["speech"]}}
  ]
}`

func TestSoundEffectModelsAreTheSpeechModelsBilledPerSecondOfGeneratedAudio(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(speechModelsBody))
	}))
	defer srv.Close()
	models, err := newMediaModelCatalog("test-key", srv.URL).Models(context.Background(), mediagen.KindSoundEffect)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("output_modalities") != "speech" {
		t.Fatalf("query %v", gotQuery)
	}
	if len(models) != 1 || models[0].ID != "bytedance-seed/seed-audio-1-0" {
		t.Fatalf("models %+v", models)
	}
}
