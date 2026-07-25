package whatsapp_business_phone

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDialog360PartnerClient_CancelChannel_UsesClientScopedCancellationRequest(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewDialog360PartnerClient(srv.URL, "partner1", "key1", "sol1", srv.Client())
	if err := c.CancelChannel("client9", "chan123"); err != nil {
		t.Fatalf("CancelChannel: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("cancellation must be a POST (graceful, reversible), got %s", gotMethod)
	}
	// Validated against the live 360dialog API: the partner-scoped form (no
	// /clients/ segment) returns 404 and never cancels, so the channel keeps
	// billing. The client-scoped form is the only one that works.
	want := "/partners/partner1/clients/client9/channels/chan123/control/cancellation_request"
	if gotPath != want {
		t.Fatalf("expected %s, got %s", want, gotPath)
	}
}

func TestDialog360PartnerClient_ReactivateChannel_UsesClientScopedPath(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewDialog360PartnerClient(srv.URL, "partner1", "key1", "sol1", srv.Client())
	if err := c.ReactivateChannel("client9", "chan123"); err != nil {
		t.Fatalf("ReactivateChannel: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	want := "/partners/partner1/clients/client9/channels/chan123/control/reactivate"
	if gotPath != want {
		t.Fatalf("expected %s, got %s", want, gotPath)
	}
}

func TestDialog360PartnerClient_CancelChannel_RequiresClientID(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewDialog360PartnerClient(srv.URL, "partner1", "key1", "sol1", srv.Client())
	if err := c.CancelChannel("", "chan123"); err == nil {
		t.Fatal("expected an error when clientID is empty (would otherwise hit a 404 path and leak)")
	}
	if hit {
		t.Fatal("must not send a request without a clientID")
	}
}

func TestDialog360PartnerClient_CancelChannel_PropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	c := NewDialog360PartnerClient(srv.URL, "partner1", "key1", "sol1", srv.Client())
	if err := c.CancelChannel("client9", "chan123"); err == nil {
		t.Fatal("expected error on non-2xx response, got nil")
	}
}
