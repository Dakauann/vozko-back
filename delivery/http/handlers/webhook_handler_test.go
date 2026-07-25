package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/webhook"
	businessphone "vozko/domain/whatsapp/business_phone"
	whatsapptemplate "vozko/domain/whatsapp/template"
)

type mockPublishWebhook struct {
	calls      []publishCall
	publishErr error
}

type publishCall struct {
	topic   string
	payload []byte
}

func (m *mockPublishWebhook) Publish(topic string, payload []byte) error {
	if m.publishErr != nil {
		return m.publishErr
	}
	m.calls = append(m.calls, publishCall{topic: topic, payload: payload})
	return nil
}

func newHandler(pub *mockPublishWebhook, asaasToken, waVerifyToken string) *WebhookHandler {
	return NewWebhookHandler(pub, asaasToken, waVerifyToken, "")
}

func TestHandleAsaasWebhook_ValidPayload(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "secret-token", "")

	body := `{"id":"evt_123","event":"PAYMENT_RECEIVED","payment":{"id":"pay_abc"}}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/asaas", strings.NewReader(body))
	req.Header.Set("asaas-access-token", "secret-token")
	rec := httptest.NewRecorder()

	h.HandleAsaasWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(pub.calls))
	}
	if pub.calls[0].topic != webhook.TopicAsaasPayment {
		t.Fatalf("expected topic %s, got %s", webhook.TopicAsaasPayment, pub.calls[0].topic)
	}
	if string(pub.calls[0].payload) != body {
		t.Fatalf("payload mismatch")
	}
}

func TestHandleAsaasWebhook_InvalidToken(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "secret-token", "")

	body := `{"event":"PAYMENT_RECEIVED"}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/asaas", strings.NewReader(body))
	req.Header.Set("asaas-access-token", "wrong-token")
	rec := httptest.NewRecorder()

	h.HandleAsaasWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if len(pub.calls) != 0 {
		t.Fatal("should not publish when token is invalid")
	}
}

func TestHandleAsaasWebhook_MissingToken(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "secret-token", "")

	body := `{"event":"PAYMENT_RECEIVED"}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/asaas", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleAsaasWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestHandleAsaasWebhook_ValidToken(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "secret-token", "")

	body := `{"event":"PAYMENT_RECEIVED","payment":{"id":"pay_1"}}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/asaas", strings.NewReader(body))
	req.Header.Set("asaas-access-token", "secret-token")
	rec := httptest.NewRecorder()

	h.HandleAsaasWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(pub.calls) != 1 {
		t.Fatal("expected 1 publish call")
	}
}

func TestHandleAsaasWebhook_InvalidJSON(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "secret-token", "")

	req := httptest.NewRequest(http.MethodPost, "/webhooks/asaas", strings.NewReader("not json"))
	req.Header.Set("asaas-access-token", "secret-token")
	rec := httptest.NewRecorder()

	h.HandleAsaasWebhook(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if len(pub.calls) != 0 {
		t.Fatal("should not publish invalid JSON")
	}
}

func TestHandleAsaasWebhook_PublishError(t *testing.T) {
	pub := &mockPublishWebhook{publishErr: errors.New("queue down")}
	h := newHandler(pub, "secret-token", "")

	body := `{"event":"PAYMENT_RECEIVED","payment":{"id":"pay_1"}}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/asaas", strings.NewReader(body))
	req.Header.Set("asaas-access-token", "secret-token")
	rec := httptest.NewRecorder()

	h.HandleAsaasWebhook(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestHandleAsaasWebhook_EmptyTokenRejectsAll(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "", "")

	body := `{"event":"PAYMENT_RECEIVED","payment":{"id":"pay_1"}}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/asaas", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleAsaasWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when no token configured, got %d", rec.Code)
	}
	if len(pub.calls) != 0 {
		t.Fatal("should not publish when no token configured")
	}
}

func TestHandleWhatsAppWebhook_Verification(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "", "my-verify-token")

	req := httptest.NewRequest(http.MethodGet, "/webhooks/whatsapp?hub.mode=subscribe&hub.challenge=abc123&hub.verify_token=my-verify-token", nil)
	rec := httptest.NewRecorder()

	h.HandleWhatsAppWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "abc123" {
		t.Fatalf("expected challenge 'abc123', got '%s'", rec.Body.String())
	}
}

func TestHandleWhatsAppWebhook_VerificationWrongToken(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "", "my-verify-token")

	req := httptest.NewRequest(http.MethodGet, "/webhooks/whatsapp?hub.mode=subscribe&hub.challenge=abc123&hub.verify_token=wrong", nil)
	rec := httptest.NewRecorder()

	h.HandleWhatsAppWebhook(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestHandleWhatsAppWebhook_VerificationNoTokenConfig(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "", "")

	req := httptest.NewRequest(http.MethodGet, "/webhooks/whatsapp?hub.mode=subscribe&hub.challenge=test-challenge", nil)
	rec := httptest.NewRecorder()

	h.HandleWhatsAppWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "test-challenge" {
		t.Fatalf("expected challenge 'test-challenge', got '%s'", rec.Body.String())
	}
}

func TestHandleWhatsAppWebhook_VerificationInvalidMode(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "", "")

	req := httptest.NewRequest(http.MethodGet, "/webhooks/whatsapp?hub.mode=unsubscribe&hub.challenge=test", nil)
	rec := httptest.NewRecorder()

	h.HandleWhatsAppWebhook(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleWhatsAppWebhook_MessageEvent(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "", "")

	payload := map[string]interface{}{
		"object": "whatsapp_business_account",
		"entry": []map[string]interface{}{
			{
				"id": "123",
				"changes": []map[string]interface{}{
					{
						"field": "messages",
						"value": map[string]interface{}{
							"messages": []map[string]interface{}{
								{"id": "wamid.123", "from": "5511999999999", "type": "text"},
							},
						},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/whatsapp", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()

	h.HandleWhatsAppWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(pub.calls))
	}
	if pub.calls[0].topic != webhook.TopicWhatsAppMessage {
		t.Fatalf("expected topic %s, got %s", webhook.TopicWhatsAppMessage, pub.calls[0].topic)
	}
}

func TestHandleWhatsAppWebhook_StatusEvent(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "", "")

	payload := map[string]interface{}{
		"object": "whatsapp_business_account",
		"entry": []map[string]interface{}{
			{
				"id": "123",
				"changes": []map[string]interface{}{
					{
						"field": "messages",
						"value": map[string]interface{}{
							"statuses": []map[string]interface{}{
								{"id": "wamid.456", "status": "delivered", "recipient_id": "5511999999999"},
							},
						},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/whatsapp", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()

	h.HandleWhatsAppWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 publish")
	}
	if pub.calls[0].topic != webhook.TopicWhatsAppMessage {
		t.Fatalf("status events with field=messages should route to message topic, got %s", pub.calls[0].topic)
	}
}

func TestHandleWhatsAppWebhook_PhoneEvent(t *testing.T) {
	phoneFields := []string{
		businessphone.FieldPhoneNumberQualityUpdate,
		businessphone.FieldPhoneNumberNameUpdate,
		businessphone.FieldAccountAlerts,
		businessphone.FieldBusinessCapabilityUpdate,
		businessphone.FieldAccountUpdate,
		businessphone.FieldAccountReviewUpdate,
		businessphone.FieldBusinessStatusUpdate,
	}

	for _, field := range phoneFields {
		pub := &mockPublishWebhook{}
		h := newHandler(pub, "", "")

		payload := map[string]interface{}{
			"object": "whatsapp_business_account",
			"entry": []map[string]interface{}{
				{
					"id": "waba-123",
					"changes": []map[string]interface{}{
						{
							"field": field,
							"value": map[string]interface{}{
								"display_phone_number": "+15556446648",
							},
						},
					},
				},
			},
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/webhooks/whatsapp", strings.NewReader(string(body)))
		rec := httptest.NewRecorder()

		h.HandleWhatsAppWebhook(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("field %s: expected 200, got %d", field, rec.Code)
		}
		if len(pub.calls) != 1 {
			t.Fatalf("field %s: expected 1 publish", field)
		}
		if pub.calls[0].topic != webhook.TopicWhatsAppPhone {
			t.Fatalf("field %s: expected topic %s, got %s", field, webhook.TopicWhatsAppPhone, pub.calls[0].topic)
		}
	}
}

func TestHandleWhatsAppWebhook_TemplateEvent(t *testing.T) {
	templateFields := []string{
		whatsapptemplate.FieldMessageTemplateStatusUpdate,
		whatsapptemplate.FieldMessageTemplateQualityUpdate,
		whatsapptemplate.FieldMessageTemplateComponentsUpdate,
		whatsapptemplate.FieldTemplateCategoryUpdate,
	}

	for _, field := range templateFields {
		pub := &mockPublishWebhook{}
		h := newHandler(pub, "", "")

		payload := map[string]interface{}{
			"object": "whatsapp_business_account",
			"entry": []map[string]interface{}{
				{
					"id": "waba-123",
					"changes": []map[string]interface{}{
						{
							"field": field,
							"value": map[string]interface{}{
								"message_template_id": 12345,
								"event":               "APPROVED",
							},
						},
					},
				},
			},
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/webhooks/whatsapp", strings.NewReader(string(body)))
		rec := httptest.NewRecorder()

		h.HandleWhatsAppWebhook(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("field %s: expected 200, got %d", field, rec.Code)
		}
		if len(pub.calls) != 1 {
			t.Fatalf("field %s: expected 1 publish", field)
		}
		if pub.calls[0].topic != webhook.TopicWhatsAppTemplate {
			t.Fatalf("field %s: expected topic %s, got %s", field, webhook.TopicWhatsAppTemplate, pub.calls[0].topic)
		}
	}
}

func TestHandleWhatsAppWebhook_UnknownFieldRoutesToMessage(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "", "")

	payload := map[string]interface{}{
		"object": "whatsapp_business_account",
		"entry": []map[string]interface{}{
			{
				"id": "123",
				"changes": []map[string]interface{}{
					{
						"field": "some_unknown_field",
						"value": map[string]interface{}{},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/whatsapp", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()

	h.HandleWhatsAppWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if pub.calls[0].topic != webhook.TopicWhatsAppMessage {
		t.Fatalf("unknown field should route to message topic, got %s", pub.calls[0].topic)
	}
}

func TestHandleWhatsAppWebhook_PublishError(t *testing.T) {
	pub := &mockPublishWebhook{publishErr: errors.New("queue down")}
	h := newHandler(pub, "", "")

	payload := map[string]interface{}{
		"object": "whatsapp_business_account",
		"entry": []map[string]interface{}{
			{
				"id":      "123",
				"changes": []map[string]interface{}{{"field": "messages", "value": map[string]interface{}{}}},
			},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/whatsapp", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()

	h.HandleWhatsAppWebhook(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestHandleWhatsAppWebhook_MethodNotAllowed(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "", "")

	req := httptest.NewRequest(http.MethodPut, "/webhooks/whatsapp", nil)
	rec := httptest.NewRecorder()

	h.HandleWhatsAppWebhook(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleWhatsAppWebhook_EmptyBody(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "", "")

	req := httptest.NewRequest(http.MethodPost, "/webhooks/whatsapp", strings.NewReader(""))
	rec := httptest.NewRecorder()

	h.HandleWhatsAppWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("empty body with no field should still route to message topic and return 200, got %d", rec.Code)
	}
}

func TestHandleWhatsAppWebhook_PreservesPayload(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newHandler(pub, "", "")

	original := `{"object":"whatsapp_business_account","entry":[{"id":"123","changes":[{"field":"messages","value":{"messages":[{"id":"wamid.999"}]}}]}]}`

	req := httptest.NewRequest(http.MethodPost, "/webhooks/whatsapp", strings.NewReader(original))
	rec := httptest.NewRecorder()

	h.HandleWhatsAppWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if string(pub.calls[0].payload) != original {
		t.Fatalf("payload should be preserved exactly as received")
	}
}

func TestExtractWebhookField_Messages(t *testing.T) {
	body := []byte(`{"entry":[{"changes":[{"field":"messages"}]}]}`)
	field := extractWebhookField(body)
	if field != "messages" {
		t.Fatalf("expected 'messages', got '%s'", field)
	}
}

func TestExtractWebhookField_PhoneQuality(t *testing.T) {
	body := []byte(`{"entry":[{"changes":[{"field":"phone_number_quality_update"}]}]}`)
	field := extractWebhookField(body)
	if field != "phone_number_quality_update" {
		t.Fatalf("expected 'phone_number_quality_update', got '%s'", field)
	}
}

func TestExtractWebhookField_TemplateStatus(t *testing.T) {
	body := []byte(`{"entry":[{"changes":[{"field":"message_template_status_update"}]}]}`)
	field := extractWebhookField(body)
	if field != "message_template_status_update" {
		t.Fatalf("expected 'message_template_status_update', got '%s'", field)
	}
}

func TestExtractWebhookField_InvalidJSON(t *testing.T) {
	body := []byte(`not json`)
	field := extractWebhookField(body)
	if field != "" {
		t.Fatalf("expected empty string for invalid JSON, got '%s'", field)
	}
}

func TestExtractWebhookField_EmptyEntry(t *testing.T) {
	body := []byte(`{"entry":[]}`)
	field := extractWebhookField(body)
	if field != "" {
		t.Fatalf("expected empty string for empty entry, got '%s'", field)
	}
}

func TestExtractWebhookField_NoChanges(t *testing.T) {
	body := []byte(`{"entry":[{"changes":[]}]}`)
	field := extractWebhookField(body)
	if field != "" {
		t.Fatalf("expected empty string for empty changes, got '%s'", field)
	}
}

func TestSecureCompare(t *testing.T) {
	if !secureCompare("abc", "abc") {
		t.Fatal("identical strings should match")
	}
	if secureCompare("abc", "abd") {
		t.Fatal("different strings should not match")
	}
	if secureCompare("abc", "abcd") {
		t.Fatal("different length strings should not match")
	}
	if secureCompare("", "a") {
		t.Fatal("empty vs non-empty should not match")
	}
	if !secureCompare("", "") {
		t.Fatal("two empty strings should match")
	}
}

// --- 360dialog inbound messaging webhook ---

const dialog360MessageBody = `{"entry":[{"changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"123"},"messages":[{"from":"5511999998888","id":"wamid.X","type":"text","text":{"body":"oi"}}]}}]}]}`

func newDialog360Handler(pub *mockPublishWebhook, secret string) *WebhookHandler {
	h := NewWebhookHandler(pub, "", "", "")
	h.SetDialog360WebhookSecret(secret)
	return h
}

func TestDialog360MessageWebhook_AuthenticatedRoutesToMessageTopic(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newDialog360Handler(pub, "shh")

	req := httptest.NewRequest(http.MethodPost, "/webhooks/360dialog/messages?secret=shh", strings.NewReader(dialog360MessageBody))
	rec := httptest.NewRecorder()
	h.HandleDialog360MessageWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(pub.calls) != 1 || pub.calls[0].topic != webhook.TopicWhatsAppMessage {
		t.Fatalf("expected 1 publish to %s, got %+v", webhook.TopicWhatsAppMessage, pub.calls)
	}
	if string(pub.calls[0].payload) != dialog360MessageBody {
		t.Fatal("payload must be forwarded verbatim")
	}
}

func TestDialog360MessageWebhook_RejectsWrongSecret(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newDialog360Handler(pub, "shh")

	for _, q := range []string{"?secret=wrong", ""} {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/360dialog/messages"+q, strings.NewReader(dialog360MessageBody))
		rec := httptest.NewRecorder()
		h.HandleDialog360MessageWebhook(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("query %q: expected 401, got %d", q, rec.Code)
		}
	}
	if len(pub.calls) != 0 {
		t.Fatalf("must not publish on auth failure, got %d", len(pub.calls))
	}
}

func TestDialog360MessageWebhook_AcceptsHeaderSecret(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newDialog360Handler(pub, "shh")

	req := httptest.NewRequest(http.MethodPost, "/webhooks/360dialog/messages", strings.NewReader(dialog360MessageBody))
	req.Header.Set("X-360Dialog-Webhook-Secret", "shh")
	rec := httptest.NewRecorder()
	h.HandleDialog360MessageWebhook(rec, req)

	if rec.Code != http.StatusOK || len(pub.calls) != 1 {
		t.Fatalf("header secret should authenticate: code=%d calls=%d", rec.Code, len(pub.calls))
	}
}

func TestDialog360MessageWebhook_UnsetSecretRejects(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newDialog360Handler(pub, "") // secret unset -> fail closed

	req := httptest.NewRequest(http.MethodPost, "/webhooks/360dialog/messages", strings.NewReader(dialog360MessageBody))
	rec := httptest.NewRecorder()
	h.HandleDialog360MessageWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unset secret must fail closed: expected 401, got %d", rec.Code)
	}
	if len(pub.calls) != 0 {
		t.Fatalf("must not publish when secret unset, got %d", len(pub.calls))
	}
}

func TestDialog360MessageWebhook_CoexistenceFieldRoutesToCoexistenceTopic(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newDialog360Handler(pub, "shh")

	body := `{"entry":[{"changes":[{"field":"history","value":{}}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/360dialog/messages?secret=shh", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleDialog360MessageWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(pub.calls) != 1 || pub.calls[0].topic != webhook.TopicWhatsAppCoexistence {
		t.Fatalf("coexistence event must route to %s, got %+v", webhook.TopicWhatsAppCoexistence, pub.calls)
	}
}

func TestDialog360MessageWebhook_GetProbeReturns200(t *testing.T) {
	pub := &mockPublishWebhook{}
	h := newDialog360Handler(pub, "shh")

	req := httptest.NewRequest(http.MethodGet, "/webhooks/360dialog/messages", nil)
	rec := httptest.NewRecorder()
	h.HandleDialog360MessageWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET probe should return 200, got %d", rec.Code)
	}
}
