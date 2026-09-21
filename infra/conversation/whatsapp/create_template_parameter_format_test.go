package whatsapp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/conversation"
)

func TestCreateTemplate_ParameterFormatOnlyForMeta(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"t1","status":"PENDING","category":"UTILITY"}`))
	}))
	defer srv.Close()

	input := conversation.CreateTemplateInput{
		Name:            "welcome",
		Language:        "pt_BR",
		Category:        "UTILITY",
		ParameterFormat: "NAMED",
		Components:      []conversation.TemplateComponent{{Type: "BODY", Text: "Bem vindo"}},
	}

	d360 := NewClient(Config{
		BaseURL: srv.URL, AccessToken: "chankey", AuthHeaderName: "D360-API-KEY",
		TemplatesChannelScoped: true,
	})
	if _, err := d360.CreateTemplate(context.Background(), input); err != nil {
		t.Fatalf("360dialog CreateTemplate: %v", err)
	}
	if strings.Contains(string(body), "parameter_format") {
		t.Fatalf("360dialog request must NOT include parameter_format (it 400s); body: %s", body)
	}

	meta := NewClient(Config{BaseURL: srv.URL, AccessToken: "tok", WABAId: "waba1"})
	if _, err := meta.CreateTemplate(context.Background(), input); err != nil {
		t.Fatalf("Meta CreateTemplate: %v", err)
	}
	if !strings.Contains(string(body), `"parameter_format":"NAMED"`) {
		t.Fatalf("Meta request MUST include parameter_format=NAMED; body: %s", body)
	}
}
