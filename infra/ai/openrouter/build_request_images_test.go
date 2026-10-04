package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/ai"
)

const visionCatalogBody = `{"data":[
  {"id":"vision/model","name":"Vision","architecture":{"input_modalities":["text","image"]}},
  {"id":"text/model","name":"Text","architecture":{"input_modalities":["text"]}}
]}`

func visionService(t *testing.T) *Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(visionCatalogBody))
	}))
	t.Cleanup(srv.Close)
	s := mustService(t, Config{DefaultModel: "text/model"}, injectFakeToolService{})
	s.catalogFetcher = newModelCatalogFetcher("test-key", srv.URL)
	return s
}

func imageInput(model string) ai.GenerateInput {
	return ai.GenerateInput{
		Model:             model,
		ToolExecutionMode: ai.ToolExecutionModeNone,
		Messages:          []ai.Message{{Role: ai.RoleUser, Content: "use o logo", Images: []string{"https://cdn.example/logo.png"}}},
	}
}

func TestBuildRequestSendsImagesAsParts(t *testing.T) {
	s := visionService(t)
	req := s.buildRequest(s.visibleInput(context.Background(), imageInput("vision/model")))
	parts := req.Messages[0].Content.Multi
	if len(parts) != 2 || parts[0].Text != "use o logo" || parts[1].ImageURL == nil || parts[1].ImageURL.URL != "https://cdn.example/logo.png" {
		t.Fatalf("parts = %+v, want the text then the image", parts)
	}
}

func TestBuildRequestKeepsTextOnlyForModelsThatCannotSeeImages(t *testing.T) {
	s := visionService(t)
	for _, model := range []string{"text/model", "unknown/model", ""} {
		req := s.buildRequest(s.visibleInput(context.Background(), imageInput(model)))
		content := req.Messages[0].Content
		if len(content.Multi) != 0 || content.Text != "use o logo" {
			t.Fatalf("model %q content = %+v, want plain text", model, content)
		}
	}
}
