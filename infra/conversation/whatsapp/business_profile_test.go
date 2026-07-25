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

// TestBusinessProfile_Dialog360_ChannelScoped verifies the 360dialog path: channel-scoped
// endpoint /whatsapp_business_profile, D360-API-KEY, and the {"data":[{...}]} response
// shape validated live against waba-v2.360dialog.io.
func TestBusinessProfile_Dialog360_ChannelScoped(t *testing.T) {
	var gotPath, gotAuth, gotBody, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotAuth = r.URL.Path, r.Method, r.Header.Get("D360-API-KEY")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"data":[{"about":"We help","email":"a@b.com","vertical":"OTHER","websites":["https://x.io"]}]}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	c := NewClient(Config{
		BaseURL: srv.URL, AccessToken: "chankey", AuthHeaderName: "D360-API-KEY",
		OmitPhoneNumberInPath: true,
	}).(*Client)

	prof, err := c.GetBusinessProfile(context.Background())
	if err != nil {
		t.Fatalf("GetBusinessProfile: %v", err)
	}
	if gotPath != "/whatsapp_business_profile" || gotMethod != http.MethodGet {
		t.Fatalf("GET must hit channel-scoped /whatsapp_business_profile, got %s %s", gotMethod, gotPath)
	}
	if gotAuth != "chankey" {
		t.Fatalf("GET must send D360-API-KEY, got %q", gotAuth)
	}
	if prof.About != "We help" || prof.Email != "a@b.com" || prof.Vertical != "OTHER" || len(prof.Websites) != 1 {
		t.Fatalf("decoded profile wrong: %+v", prof)
	}

	update := conversation.WhatsAppBusinessProfile{
		About: "About us", Email: "hi@acme.io", Vertical: "PROF_SERVICES",
		Websites: []string{"https://acme.io"},
	}
	if err := c.UpdateBusinessProfile(context.Background(), update); err != nil {
		t.Fatalf("UpdateBusinessProfile: %v", err)
	}
	if gotPath != "/whatsapp_business_profile" || gotMethod != http.MethodPost {
		t.Fatalf("POST must hit /whatsapp_business_profile, got %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(gotBody, `"messaging_product":"whatsapp"`) || !strings.Contains(gotBody, `"about":"About us"`) || !strings.Contains(gotBody, `"vertical":"PROF_SERVICES"`) {
		t.Fatalf("update body missing required fields; got %s", gotBody)
	}
}

// The Meta path scopes by phone number id.
func TestBusinessProfile_Meta_PhoneScoped(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"data":[{"about":"x"}]}`))
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, PhoneNumberID: "555", AccessToken: "tok", WABAId: "w"}).(*Client)
	if _, err := c.GetBusinessProfile(context.Background()); err != nil {
		t.Fatalf("GetBusinessProfile: %v", err)
	}
	if gotPath != "/555/whatsapp_business_profile" {
		t.Fatalf("Meta profile must hit /{phone}/whatsapp_business_profile, got %s", gotPath)
	}
}
