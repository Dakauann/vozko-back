package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
)

type metaEditServer struct {
	stored   string
	edits    [][]byte
	editPath string
}

func newMetaEditServer(t *testing.T, stored string) (*metaEditServer, conversation.WhatsAppClient) {
	t.Helper()
	server := &metaEditServer{stored: stored}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(server.stored))
			return
		}
		body, _ := io.ReadAll(r.Body)
		server.edits = append(server.edits, body)
		server.editPath = r.URL.Path
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(srv.Close)
	return server, NewClient(Config{BaseURL: srv.URL, AccessToken: "tok", WABAId: "waba1", PhoneNumberID: "phone1"})
}

func editedComponents() []conversation.TemplateComponent {
	return []conversation.TemplateComponent{
		{Type: "HEADER", Format: "IMAGE", Example: &conversation.TemplateExample{HeaderHandle: []string{"4::fresh"}}},
		{Type: "BODY", Text: "Oi {{1}}, nova oferta.", Example: &conversation.TemplateExample{BodyText: [][]string{{"Ana"}}}},
		{Type: "FOOTER", Text: "Responda /sair"},
		{Type: "BUTTONS", Buttons: []conversation.TemplateButton{{Type: "QUICK_REPLY", Text: "Quero"}, {Type: "URL", Text: "Ver", URL: "https://loja.com/{{1}}", Example: "https://loja.com/a"}}},
	}
}

func decodeEdit(t *testing.T, body []byte) (string, []map[string]any) {
	t.Helper()
	var payload struct {
		Category   string           `json:"category"`
		Components []map[string]any `json:"components"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return payload.Category, payload.Components
}

func TestAnEditSendsEveryComponentAndKeepsWhatVozkoDoesNotModel(t *testing.T) {
	server, client := newMetaEditServer(t, `{"id":"x1","status":"APPROVED","components":[
		{"type":"HEADER","format":"IMAGE","example":{"header_handle":["https://scontent.example/old.jpg"]}},
		{"type":"BODY","text":"Oi {{1}}","example":{"body_text":[["Ana"]]}},
		{"type":"LIMITED_TIME_OFFER","limited_time_offer":{"text":"Expira","has_expiration":true}},
		{"type":"BUTTONS","buttons":[{"type":"QUICK_REPLY","text":"Quero"}]}
	]}`)

	err := client.EditTemplate(context.Background(), "x1", conversation.EditTemplateInput{Components: editedComponents()})
	if err != nil {
		t.Fatalf("EditTemplate: %v", err)
	}

	if len(server.edits) != 1 || server.editPath != "/x1" {
		t.Fatalf("edits = %d at %s", len(server.edits), server.editPath)
	}
	category, components := decodeEdit(t, server.edits[0])
	if category != "" {
		t.Fatalf("an unchanged category is not sent: %q", category)
	}
	types := make([]string, len(components))
	for i, comp := range components {
		types[i], _ = comp["type"].(string)
	}
	if strings.Join(types, ",") != "HEADER,BODY,FOOTER,BUTTONS,LIMITED_TIME_OFFER" {
		t.Fatalf("types = %v", types)
	}
	header, _ := json.Marshal(components[0])
	if !strings.Contains(string(header), `"header_handle":["4::fresh"]`) {
		t.Fatalf("header = %s", header)
	}
	offer, _ := json.Marshal(components[4])
	if string(offer) != `{"limited_time_offer":{"has_expiration":true,"text":"Expira"},"type":"LIMITED_TIME_OFFER"}` {
		t.Fatalf("offer = %s", offer)
	}
	buttons, _ := json.Marshal(components[3])
	if !strings.Contains(string(buttons), `"example":["https://loja.com/a"]`) {
		t.Fatalf("buttons = %s", buttons)
	}
}

func TestAnEditRefusesToReplaceButtonsVozkoCannotModel(t *testing.T) {
	server, client := newMetaEditServer(t, `{"id":"x1","components":[
		{"type":"BODY","text":"Oi"},
		{"type":"BUTTONS","buttons":[{"type":"FLOW","text":"Abrir","flow_id":"123","flow_action":"navigate"}]}
	]}`)

	err := client.EditTemplate(context.Background(), "x1", conversation.EditTemplateInput{Components: editedComponents()})

	if !errors.Is(err, template.ErrEditUnsupported) {
		t.Fatalf("err = %v", err)
	}
	if len(server.edits) != 0 {
		t.Fatal("nothing may be sent when a part would be lost")
	}
}

func TestAnEditCarriesAChangedCategory(t *testing.T) {
	server, client := newMetaEditServer(t, `{"id":"x1","components":[{"type":"BODY","text":"Oi"}]}`)

	err := client.EditTemplate(context.Background(), "x1", conversation.EditTemplateInput{Category: "UTILITY", Components: []conversation.TemplateComponent{{Type: "BODY", Text: "Seu pedido saiu."}}})

	if err != nil {
		t.Fatal(err)
	}
	if category, _ := decodeEdit(t, server.edits[0]); category != "UTILITY" {
		t.Fatalf("category = %q", category)
	}
}

func TestAChannelScopedClientCannotEditTemplates(t *testing.T) {
	client := &Client{accessToken: "tok", templatesChannelScoped: true}

	err := client.EditTemplate(context.Background(), "x1", conversation.EditTemplateInput{Components: editedComponents()})

	if !errors.Is(err, template.ErrEditUnsupported) {
		t.Fatalf("err = %v", err)
	}
}
