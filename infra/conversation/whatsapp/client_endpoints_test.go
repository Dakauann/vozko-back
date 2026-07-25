package whatsapp

import (
	"testing"

	"vozko/domain/conversation"
)

// metaClient and dialog360Client build the two provider variants the factory
// produces, so the endpoint builders are exercised exactly as in production.
func metaClient() *Client {
	return NewClient(Config{
		BaseURL:       "https://graph.facebook.com/v22.0",
		PhoneNumberID: "PNID",
		WABAId:        "WABA",
		AccessToken:   "tok",
	}).(*Client)
}

func dialog360Client() *Client {
	return NewClient(Config{
		BaseURL:                "https://waba-v2.360dialog.io",
		PhoneNumberID:          "PNID",
		WABAId:                 "WABA",
		AccessToken:            "key",
		AuthHeaderName:         "D360-API-KEY",
		OmitPhoneNumberInPath:  true,
		TemplatesChannelScoped: true,
	}).(*Client)
}

func TestEndpoints_Meta(t *testing.T) {
	c := metaClient()
	// Guards against the infinite-recursion regression in the non-omit branch.
	if got, want := c.messagesEndpoint(), "https://graph.facebook.com/v22.0/PNID/messages"; got != want {
		t.Fatalf("messagesEndpoint = %q, want %q", got, want)
	}
	if got, want := c.mediaEndpoint(), "https://graph.facebook.com/v22.0/PNID/media"; got != want {
		t.Fatalf("mediaEndpoint = %q, want %q", got, want)
	}
	if got, want := c.templatesCollectionEndpoint(), "https://graph.facebook.com/v22.0/WABA/message_templates"; got != want {
		t.Fatalf("templatesCollectionEndpoint = %q, want %q", got, want)
	}
}

func TestEndpoints_Dialog360(t *testing.T) {
	c := dialog360Client()
	if got, want := c.messagesEndpoint(), "https://waba-v2.360dialog.io/messages"; got != want {
		t.Fatalf("messagesEndpoint = %q, want %q", got, want)
	}
	if got, want := c.mediaEndpoint(), "https://waba-v2.360dialog.io/media"; got != want {
		t.Fatalf("mediaEndpoint = %q, want %q", got, want)
	}
	if got, want := c.templatesCollectionEndpoint(), "https://waba-v2.360dialog.io/v1/configs/templates"; got != want {
		t.Fatalf("templatesCollectionEndpoint = %q, want %q", got, want)
	}
}

// mapTemplateResponse must key 360dialog templates (which carry no numeric id) by
// their name, while leaving Meta's numeric id untouched.
func TestMapTemplateResponse_IDFallsBackToName(t *testing.T) {
	meta := mapTemplateResponse(templateResponse{ID: "123", Name: "promo"})
	if meta.ID != "123" {
		t.Fatalf("meta id = %q, want 123", meta.ID)
	}
	d360 := mapTemplateResponse(templateResponse{ID: "", Name: "promo"})
	if d360.ID != "promo" {
		t.Fatalf("dialog360 id = %q, want fallback to name 'promo'", d360.ID)
	}
}

var _ conversation.WhatsAppClient = (*Client)(nil)
