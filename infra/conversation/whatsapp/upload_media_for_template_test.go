package whatsapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/conversation"
)

func newUploadTestServer(t *testing.T, wantSessionPath string, onSession, onUpload func(r *http.Request)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(wantSessionPath, func(w http.ResponseWriter, r *http.Request) {
		onSession(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"upload:SESSION123"}`))
	})
	mux.HandleFunc("/upload:SESSION123", func(w http.ResponseWriter, r *http.Request) {
		onUpload(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"h":"4::HANDLE"}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		http.NotFound(w, r)
	})
	return httptest.NewServer(mux)
}

var pngBytes = []byte("\x89PNG\r\n\x1a\n0000000000")

func TestUploadMediaForTemplate_Dialog360(t *testing.T) {
	var sessionReq, uploadReq *http.Request
	srv := newUploadTestServer(t, "/uploads",
		func(r *http.Request) { sessionReq = r },
		func(r *http.Request) { uploadReq = r },
	)
	defer srv.Close()

	c := NewClient(Config{
		BaseURL:                srv.URL,
		PhoneNumberID:          "PNID",
		AccessToken:            "d360key",
		AuthHeaderName:         "D360-API-KEY",
		OmitPhoneNumberInPath:  true,
		TemplatesChannelScoped: true,
		HTTPClient:             srv.Client(),
	})

	handle, err := c.UploadMediaForTemplate(context.Background(), conversation.UploadMediaForTemplateInput{
		Data:     pngBytes,
		FileName: "promo.png",
	})
	if err != nil {
		t.Fatalf("UploadMediaForTemplate: %v", err)
	}
	if handle != "4::HANDLE" {
		t.Fatalf("handle = %q, want 4::HANDLE", handle)
	}

	if sessionReq == nil {
		t.Fatal("session request never made")
	}
	q := sessionReq.URL.Query()
	if q.Get("file_length") == "" || q.Get("file_type") != "image/png" {
		t.Fatalf("session query = %q, want file_length and file_type=image/png", sessionReq.URL.RawQuery)
	}
	if q.Get("file_name") != "" {
		t.Fatalf("360dialog session must not send file_name (undocumented on the proxy), got %q", q.Get("file_name"))
	}
	if got := sessionReq.Header.Get("D360-API-KEY"); got != "d360key" {
		t.Fatalf("session D360-API-KEY = %q, want d360key", got)
	}
	if got := sessionReq.Header.Get("Authorization"); got != "" {
		t.Fatalf("session must not carry Authorization for 360dialog, got %q", got)
	}

	if uploadReq == nil {
		t.Fatal("upload request never made")
	}
	if got := uploadReq.Header.Get("D360-API-KEY"); got != "d360key" {
		t.Fatalf("upload D360-API-KEY = %q, want d360key", got)
	}
	if got := uploadReq.Header.Get("Authorization"); got != "" {
		t.Fatalf("upload must not carry Authorization for 360dialog, got %q", got)
	}
	if got := uploadReq.Header.Get("file_offset"); got != "0" {
		t.Fatalf("upload file_offset = %q, want 0", got)
	}
}

func TestUploadMediaForTemplate_Meta(t *testing.T) {
	var sessionReq, uploadReq *http.Request
	srv := newUploadTestServer(t, "/APP123/uploads",
		func(r *http.Request) { sessionReq = r },
		func(r *http.Request) { uploadReq = r },
	)
	defer srv.Close()

	c := NewClient(Config{
		BaseURL:       srv.URL,
		PhoneNumberID: "PNID",
		WABAId:        "WABA",
		AccessToken:   "metatok",
		AppID:         "APP123",
		HTTPClient:    srv.Client(),
	})

	handle, err := c.UploadMediaForTemplate(context.Background(), conversation.UploadMediaForTemplateInput{
		Data:     pngBytes,
		FileName: "promo.png",
	})
	if err != nil {
		t.Fatalf("UploadMediaForTemplate: %v", err)
	}
	if handle != "4::HANDLE" {
		t.Fatalf("handle = %q, want 4::HANDLE", handle)
	}

	if sessionReq == nil {
		t.Fatal("session request never made")
	}
	q := sessionReq.URL.Query()
	if q.Get("file_name") != "promo.png" || q.Get("file_type") != "image/png" {
		t.Fatalf("session query = %q, want file_name=promo.png and file_type=image/png", sessionReq.URL.RawQuery)
	}
	if got := sessionReq.Header.Get("Authorization"); got != "Bearer metatok" {
		t.Fatalf("session Authorization = %q, want Bearer metatok", got)
	}

	if uploadReq == nil {
		t.Fatal("upload request never made")
	}
	if got := uploadReq.Header.Get("Authorization"); got != "OAuth metatok" {
		t.Fatalf("upload Authorization = %q, want OAuth metatok", got)
	}
}

func TestUploadMediaForTemplate_MetaStillRequiresAppID(t *testing.T) {
	c := metaClient()
	_, err := c.UploadMediaForTemplate(context.Background(), conversation.UploadMediaForTemplateInput{Data: pngBytes})
	if err == nil {
		t.Fatal("expected error for Meta client without App ID")
	}
}

func TestUploadMediaForTemplate_Dialog360NeedsNoAppID(t *testing.T) {
	c := dialog360Client()
	_, err := c.UploadMediaForTemplate(context.Background(), conversation.UploadMediaForTemplateInput{})
	if err == nil || err.Error() != "either URL or Data must be provided" {
		t.Fatalf("expected to pass the config guard and fail on missing input, got: %v", err)
	}
}
