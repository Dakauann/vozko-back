package whatsapp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCalling_Dialog360_ChannelScoped verifies the 360dialog path: channel-scoped
// endpoint /calling/settings, D360-API-KEY auth, Meta-shaped body/response (validated
// live against waba-v2.360dialog.io).
func TestCalling_Dialog360_ChannelScoped(t *testing.T) {
	var gotPath, gotAuth, gotBody, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotAuth = r.URL.Path, r.Method, r.Header.Get("D360-API-KEY")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"calling":{"status":"ENABLED","call_icon_visibility":"DEFAULT"}}`))
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
	})
	calling := c.(*Client)

	enabled, err := calling.GetCallingStatus(context.Background())
	if err != nil {
		t.Fatalf("GetCallingStatus: %v", err)
	}
	if !enabled {
		t.Fatal("expected enabled=true for status ENABLED")
	}
	if gotPath != "/calling/settings" || gotMethod != http.MethodGet {
		t.Fatalf("GET must hit /calling/settings, got %s %s", gotMethod, gotPath)
	}
	if gotAuth != "chankey" {
		t.Fatalf("GET must send D360-API-KEY, got %q", gotAuth)
	}

	if err := calling.SetCallingStatus(context.Background(), true); err != nil {
		t.Fatalf("SetCallingStatus: %v", err)
	}
	if gotPath != "/calling/settings" || gotMethod != http.MethodPost {
		t.Fatalf("POST must hit /calling/settings, got %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(gotBody, `"status":"ENABLED"`) || !strings.Contains(gotBody, `"messaging_product":"whatsapp"`) {
		t.Fatalf("enable body must set calling.status=ENABLED with messaging_product; got %s", gotBody)
	}

	// Disable path sends DISABLED.
	if err := calling.SetCallingStatus(context.Background(), false); err != nil {
		t.Fatalf("SetCallingStatus(false): %v", err)
	}
	if !strings.Contains(gotBody, `"status":"DISABLED"`) {
		t.Fatalf("disable body must set calling.status=DISABLED; got %s", gotBody)
	}
}

// NOT_SET (and DISABLED) must read as not-enabled.
func TestCalling_NotSetReadsDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"calling":{"status":"NOT_SET"}}`))
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, AccessToken: "k", AuthHeaderName: "D360-API-KEY", OmitPhoneNumberInPath: true}).(*Client)
	enabled, err := c.GetCallingStatus(context.Background())
	if err != nil || enabled {
		t.Fatalf("NOT_SET must read as disabled; enabled=%v err=%v", enabled, err)
	}
}

// The Meta path (no OmitPhoneNumberInPath) scopes by phone number id: /{id}/settings.
func TestCalling_Meta_PhoneScopedEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"calling":{"status":"DISABLED"}}`))
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, PhoneNumberID: "1238425109354177", AccessToken: "tok", WABAId: "w"}).(*Client)
	if _, err := c.GetCallingStatus(context.Background()); err != nil {
		t.Fatalf("GetCallingStatus: %v", err)
	}
	if gotPath != "/1238425109354177/settings" {
		t.Fatalf("Meta calling must hit /{phone_number_id}/settings, got %s", gotPath)
	}
}
