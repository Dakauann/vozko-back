package whatsapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/conversation"
)

// These pin the two wire shapes Meta documents for authentication templates.
// They are the whole reason the feature failed before: the code went out in the
// body with nothing on the button, and Meta answered 132000.

func captureRequest(t *testing.T, response string) (conversation.WhatsAppClient, *[]byte) {
	t.Helper()
	captured := new([]byte)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*captured = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return NewClient(Config{BaseURL: srv.URL, AccessToken: "tok", WABAId: "waba1", PhoneNumberID: "phone1"}), captured
}

// The one-time code appears twice: once as the body variable and once as the
// button parameter. Meta requires both; sending only the body is a parameter
// count mismatch.
func TestSendTemplateMessage_AuthenticationCarriesTheButtonParameter(t *testing.T) {
	client, body := captureRequest(t, `{"messages":[{"id":"wamid.1"}]}`)

	_, err := client.SendTemplateMessage(context.Background(), conversation.SendTemplateMessageInput{
		To:           "5511987654321",
		TemplateName: "codigo_verificacao",
		Language:     "pt_BR",
		Parameters:   []string{"482913"},
		Buttons: []conversation.TemplateButtonParam{{
			SubType: conversation.TemplateButtonSubTypeURL,
			Index:   0,
			Text:    "482913",
		}},
	})
	if err != nil {
		t.Fatalf("SendTemplateMessage: %v", err)
	}

	var payload struct {
		Template struct {
			Components []struct {
				Type       string `json:"type"`
				SubType    string `json:"sub_type"`
				Index      string `json:"index"`
				Parameters []struct {
					Type       string `json:"type"`
					Text       string `json:"text"`
					CouponCode string `json:"coupon_code"`
				} `json:"parameters"`
			} `json:"components"`
		} `json:"template"`
	}
	if err := json.Unmarshal(*body, &payload); err != nil {
		t.Fatalf("unmarshal request: %v (body: %s)", err, *body)
	}

	if len(payload.Template.Components) != 2 {
		t.Fatalf("components = %d, want body + button; body: %s", len(payload.Template.Components), *body)
	}

	btn := payload.Template.Components[1]
	if btn.Type != "button" {
		t.Errorf("type = %q, want button", btn.Type)
	}
	if btn.SubType != "url" {
		t.Errorf("sub_type = %q, want url", btn.SubType)
	}
	// Meta's own authentication examples send the index as a string. Graph
	// coerces either way, so this is about matching the documented form.
	if btn.Index != "0" {
		t.Errorf("index = %q, want \"0\"", btn.Index)
	}
	if len(btn.Parameters) != 1 || btn.Parameters[0].Type != "text" || btn.Parameters[0].Text != "482913" {
		t.Errorf("button parameters = %+v, want one text parameter carrying the code", btn.Parameters)
	}
	if btn.Parameters[0].CouponCode != "" {
		t.Errorf("an authentication button must not send coupon_code, got %q", btn.Parameters[0].CouponCode)
	}
}

// A coupon copy-code button in a marketing template is a different shape: a
// copy_code sub-type carrying a coupon_code parameter. Pinned beside the OTP
// case so the two cannot be quietly merged.
func TestSendTemplateMessage_CouponCopyCodeUsesCouponParameter(t *testing.T) {
	client, body := captureRequest(t, `{"messages":[{"id":"wamid.1"}]}`)

	_, err := client.SendTemplateMessage(context.Background(), conversation.SendTemplateMessageInput{
		To:           "5511987654321",
		TemplateName: "cupom_outubro",
		Language:     "pt_BR",
		Buttons: []conversation.TemplateButtonParam{{
			SubType:    conversation.TemplateButtonSubTypeCopyCode,
			Index:      1,
			CouponCode: "25OFF",
		}},
	})
	if err != nil {
		t.Fatalf("SendTemplateMessage: %v", err)
	}

	var payload struct {
		Template struct {
			Components []struct {
				Type       string `json:"type"`
				SubType    string `json:"sub_type"`
				Index      string `json:"index"`
				Parameters []struct {
					Type       string `json:"type"`
					Text       string `json:"text"`
					CouponCode string `json:"coupon_code"`
				} `json:"parameters"`
			} `json:"components"`
		} `json:"template"`
	}
	if err := json.Unmarshal(*body, &payload); err != nil {
		t.Fatalf("unmarshal request: %v (body: %s)", err, *body)
	}

	if len(payload.Template.Components) != 1 {
		t.Fatalf("components = %d, want the button alone; body: %s", len(payload.Template.Components), *body)
	}
	btn := payload.Template.Components[0]
	if btn.SubType != "copy_code" || btn.Index != "1" {
		t.Errorf("sub_type/index = %q/%q, want copy_code/\"1\"", btn.SubType, btn.Index)
	}
	if len(btn.Parameters) != 1 || btn.Parameters[0].Type != "coupon_code" || btn.Parameters[0].CouponCode != "25OFF" {
		t.Errorf("parameters = %+v, want one coupon_code parameter", btn.Parameters)
	}
	if btn.Parameters[0].Text != "" {
		t.Errorf("a coupon button must not send text, got %q", btn.Parameters[0].Text)
	}
}

// The overwhelming majority of templates have no parameterized button. They
// must keep sending exactly what they sent before.
func TestSendTemplateMessage_NoButtonsMeansNoButtonComponent(t *testing.T) {
	client, body := captureRequest(t, `{"messages":[{"id":"wamid.1"}]}`)

	_, err := client.SendTemplateMessage(context.Background(), conversation.SendTemplateMessageInput{
		To:           "5511987654321",
		TemplateName: "boas_vindas",
		Language:     "pt_BR",
		Parameters:   []string{"Marina"},
	})
	if err != nil {
		t.Fatalf("SendTemplateMessage: %v", err)
	}

	var payload struct {
		Template struct {
			Components []struct {
				Type string `json:"type"`
			} `json:"components"`
		} `json:"template"`
	}
	if err := json.Unmarshal(*body, &payload); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	for _, c := range payload.Template.Components {
		if c.Type == "button" {
			t.Fatalf("unexpected button component; body: %s", *body)
		}
	}
}

// Create: Meta takes the OTP button as type OTP with an otp_type, the security
// line as a flag on BODY and the expiry as a number on FOOTER.
func TestCreateTemplate_AuthenticationShape(t *testing.T) {
	client, body := captureRequest(t, `{"id":"t1","status":"PENDING","category":"AUTHENTICATION"}`)

	recommend := true
	expiry := 10
	_, err := client.CreateTemplate(context.Background(), conversation.CreateTemplateInput{
		Name:     "codigo_verificacao",
		Language: "pt_BR",
		Category: "AUTHENTICATION",
		Components: []conversation.TemplateComponent{
			{Type: "BODY", AddSecurityRecommendation: &recommend},
			{Type: "FOOTER", CodeExpirationMinutes: &expiry},
			{Type: "BUTTONS", Buttons: []conversation.TemplateButton{
				{Type: "OTP", OTPType: "COPY_CODE"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	var payload struct {
		Components []struct {
			Type                      string `json:"type"`
			Text                      string `json:"text"`
			AddSecurityRecommendation *bool  `json:"add_security_recommendation"`
			CodeExpirationMinutes     *int   `json:"code_expiration_minutes"`
			Buttons                   []struct {
				Type    string `json:"type"`
				OTPType string `json:"otp_type"`
			} `json:"buttons"`
		} `json:"components"`
	}
	if err := json.Unmarshal(*body, &payload); err != nil {
		t.Fatalf("unmarshal request: %v (body: %s)", err, *body)
	}
	if len(payload.Components) != 3 {
		t.Fatalf("components = %d, want 3; body: %s", len(payload.Components), *body)
	}

	if payload.Components[0].AddSecurityRecommendation == nil || !*payload.Components[0].AddSecurityRecommendation {
		t.Errorf("BODY add_security_recommendation missing; body: %s", *body)
	}
	if payload.Components[1].CodeExpirationMinutes == nil || *payload.Components[1].CodeExpirationMinutes != 10 {
		t.Errorf("FOOTER code_expiration_minutes missing; body: %s", *body)
	}
	if len(payload.Components[2].Buttons) != 1 {
		t.Fatalf("buttons = %d, want 1", len(payload.Components[2].Buttons))
	}
	if payload.Components[2].Buttons[0].Type != "OTP" || payload.Components[2].Buttons[0].OTPType != "COPY_CODE" {
		t.Errorf("button = %+v, want OTP/COPY_CODE", payload.Components[2].Buttons[0])
	}
}

// A non-authentication template must not gain either field, or Meta rejects the
// whole create.
func TestCreateTemplate_OrdinaryTemplateOmitsAuthenticationFields(t *testing.T) {
	client, body := captureRequest(t, `{"id":"t1","status":"PENDING","category":"UTILITY"}`)

	_, err := client.CreateTemplate(context.Background(), conversation.CreateTemplateInput{
		Name:     "pedido_enviado",
		Language: "pt_BR",
		Category: "UTILITY",
		Components: []conversation.TemplateComponent{
			{Type: "BODY", Text: "Seu pedido saiu para entrega."},
		},
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	raw := string(*body)
	for _, field := range []string{"add_security_recommendation", "code_expiration_minutes", "otp_type"} {
		if strings.Contains(raw, field) {
			t.Errorf("%s must be absent for a utility template; body: %s", field, raw)
		}
	}
}

// Reading a template back must preserve the otp_type, or the next send cannot
// tell a code button from a quick reply.
func TestListTemplates_PreservesOTPType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{
			"id":"t1","name":"codigo_verificacao","status":"APPROVED",
			"category":"AUTHENTICATION","language":"pt_BR",
			"components":[
				{"type":"BODY","text":"{{1}} é o seu código de verificação.","add_security_recommendation":true},
				{"type":"FOOTER","text":"Este código expira em 10 minutos.","code_expiration_minutes":10},
				{"type":"BUTTONS","buttons":[{"type":"OTP","otp_type":"COPY_CODE","text":"Copiar código"}]}
			]}]}`))
	}))
	defer srv.Close()

	client := NewClient(Config{BaseURL: srv.URL, AccessToken: "tok", WABAId: "waba1"})
	out, err := client.ListTemplates(context.Background(), conversation.ListTemplatesInput{})
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(out.Templates) != 1 {
		t.Fatalf("templates = %d, want 1", len(out.Templates))
	}

	tmpl := out.Templates[0]
	var buttons []conversation.TemplateButton
	for _, c := range tmpl.Components {
		switch c.Type {
		case "BODY":
			if c.AddSecurityRecommendation == nil || !*c.AddSecurityRecommendation {
				t.Error("BODY add_security_recommendation lost on read")
			}
		case "FOOTER":
			if c.CodeExpirationMinutes == nil || *c.CodeExpirationMinutes != 10 {
				t.Error("FOOTER code_expiration_minutes lost on read")
			}
		case "BUTTONS":
			buttons = c.Buttons
		}
	}
	if len(buttons) != 1 {
		t.Fatalf("buttons = %d, want 1", len(buttons))
	}
	if buttons[0].Type != "OTP" || buttons[0].OTPType != "COPY_CODE" {
		t.Errorf("button = %+v, want OTP/COPY_CODE", buttons[0])
	}
}
