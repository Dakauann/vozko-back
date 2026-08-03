package metaembeddedsignup

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
)

type fakeD360Onboarder struct{}

func (fakeD360Onboarder) StartProvisioning(in businessphone.Dialog360ProvisionInput) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (fakeD360Onboarder) Retry(phoneID, ownerWorkspaceID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (fakeD360Onboarder) Finalize(channelID string, now time.Time) error { return nil }

type fakeTemplateWebhook struct {
	payloads []*template.TemplateWebhookPayload
}

func (f *fakeTemplateWebhook) Execute(p *template.TemplateWebhookPayload) error {
	f.payloads = append(f.payloads, p)
	return nil
}

// A real waba_template_status_changed body (360dialog partner webhook shape).
const d360TemplateStatusBody = `{
  "id": "evt1",
  "event": "waba_template_status_changed",
  "data": {
    "id": "2044166682973889",
    "template": {
      "id": "gqh7gkLf8nZPcY4EpEbmWT",
      "external_id": "1786649272200251",
      "name": "concluirinscricao_03",
      "new_status": "REJECTED",
      "language": "pt_BR",
      "rejected_reason": "INVALID_FORMAT"
    }
  }
}`

func TestParseDialog360TemplateStatus_ObjectAndArray(t *testing.T) {
	// Single object.
	got := parseDialog360TemplateStatusPayloads([]byte(d360TemplateStatusBody))
	if len(got) != 1 {
		t.Fatalf("object: got %d payloads, want 1", len(got))
	}
	v := got[0].Entry[0].Changes[0].Value
	if got[0].Entry[0].ID != "2044166682973889" {
		t.Errorf("waba id = %q, want 2044166682973889", got[0].Entry[0].ID)
	}
	if v.ChannelExternalID != "gqh7gkLf8nZPcY4EpEbmWT" {
		t.Errorf("ChannelExternalID = %q, want the 360dialog id (data.template.id)", v.ChannelExternalID)
	}
	if v.Event != "REJECTED" {
		t.Errorf("Event = %q, want REJECTED (data.template.new_status)", v.Event)
	}
	if got[0].Entry[0].Changes[0].Field != template.FieldMessageTemplateStatusUpdate {
		t.Errorf("field = %q, want status update", got[0].Entry[0].Changes[0].Field)
	}

	// Array envelope.
	arr := []byte("[" + d360TemplateStatusBody + "]")
	if g := parseDialog360TemplateStatusPayloads(arr); len(g) != 1 || g[0].Entry[0].Changes[0].Value.ChannelExternalID != "gqh7gkLf8nZPcY4EpEbmWT" {
		t.Fatalf("array: got %d payloads (want 1) with the channel id", len(g))
	}

	// Non-template partner events yield nothing.
	other := `{"event":"channel_live","data":{"id":"chan1"}}`
	if g := parseDialog360TemplateStatusPayloads([]byte(other)); len(g) != 0 {
		t.Errorf("non-template event produced %d payloads, want 0", len(g))
	}

	// Missing new_status is skipped (nothing to apply).
	noStatus := `{"event":"waba_template_status_changed","data":{"id":"w","template":{"id":"x","name":"n"}}}`
	if g := parseDialog360TemplateStatusPayloads([]byte(noStatus)); len(g) != 0 {
		t.Errorf("missing new_status produced %d payloads, want 0", len(g))
	}
}

func TestHandleDialog360PartnerWebhook_RoutesTemplateStatus(t *testing.T) {
	ftw := &fakeTemplateWebhook{}
	h := NewMetaEmbeddedSignupHandler(MetaEmbeddedSignupConfig{}).
		WithDialog360(fakeD360Onboarder{}, "s3cr3t").
		WithTemplateWebhook(ftw)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/360dialog/partner", bytes.NewReader([]byte(d360TemplateStatusBody)))
	req.Header.Set("X-360Dialog-Webhook-Secret", "s3cr3t")
	rec := httptest.NewRecorder()

	h.HandleDialog360PartnerWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(ftw.payloads) != 1 {
		t.Fatalf("template handler called %d times, want 1 (event was dropped, the bug)", len(ftw.payloads))
	}
	v := ftw.payloads[0].Entry[0].Changes[0].Value
	if v.ChannelExternalID != "gqh7gkLf8nZPcY4EpEbmWT" || v.Event != "REJECTED" {
		t.Errorf("routed value = {id:%q, event:%q}, want {gqh7gkLf8nZPcY4EpEbmWT, REJECTED}", v.ChannelExternalID, v.Event)
	}
}

func TestHandleDialog360PartnerWebhook_RejectsBadSecret(t *testing.T) {
	ftw := &fakeTemplateWebhook{}
	h := NewMetaEmbeddedSignupHandler(MetaEmbeddedSignupConfig{}).
		WithDialog360(fakeD360Onboarder{}, "s3cr3t").
		WithTemplateWebhook(ftw)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/360dialog/partner", bytes.NewReader([]byte(d360TemplateStatusBody)))
	req.Header.Set("X-360Dialog-Webhook-Secret", "wrong")
	rec := httptest.NewRecorder()

	h.HandleDialog360PartnerWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for bad secret", rec.Code)
	}
	if len(ftw.payloads) != 0 {
		t.Errorf("template handler must not run on auth failure; ran %d times", len(ftw.payloads))
	}
}
