package metaembeddedsignup

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
)

type recordingTransport struct {
	mu   sync.Mutex
	urls []string
}

func (t *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.urls = append(t.urls, r.Method+" "+r.URL.Path)
	t.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

func (t *recordingTransport) hit(fragment string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, u := range t.urls {
		if strings.Contains(u, fragment) {
			return true
		}
	}
	return false
}

type fakeOnboardUC struct {
	authorizeErr error
	executeErr   error
	executed     int
	authorized   []string
}

func (f *fakeOnboardUC) Authorize(workspaceID, metaPhoneNumberID string) error {
	f.authorized = append(f.authorized, workspaceID+"/"+metaPhoneNumberID)
	return f.authorizeErr
}

func (f *fakeOnboardUC) Execute(in businessphone.OnboardEmbeddedSignupInput) (*businessphone.OnboardEmbeddedSignupResult, error) {
	f.executed++
	if f.executeErr != nil {
		return nil, f.executeErr
	}
	return &businessphone.OnboardEmbeddedSignupResult{
		Phone: &businessphone.WhatsAppBusinessPhoneNumber{ID: "p1", MetaPhoneNumberID: in.PhoneNumberID, Status: businessphone.StatusConnected},
		IsNew: true,
	}, nil
}

func newQuotaHandler(uc *fakeOnboardUC) (*MetaEmbeddedSignupHandler, *recordingTransport) {
	transport := &recordingTransport{}
	h := NewMetaEmbeddedSignupHandler(MetaEmbeddedSignupConfig{AppID: "APPID"}).
		WithMeta("secret", uc, nil, nil, false)
	h.httpClient = &http.Client{Transport: transport}
	return h, transport
}

func runMetaOnboarding(h *MetaEmbeddedSignupHandler) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth/meta/embedded?workspace_id=ws-1", nil)
	h.handleMetaOnboarding(rec, req, EmbeddedSignupCallbackRequest{
		AccessToken:   "token",
		PhoneNumberID: "meta-1",
		WABAID:        "waba-1",
	}, "ws-1", "user-1")
	return rec
}

func TestMetaOnboarding_UnderQuotaProceeds(t *testing.T) {
	uc := &fakeOnboardUC{}
	h, transport := newQuotaHandler(uc)

	rec := runMetaOnboarding(h)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if uc.executed != 1 {
		t.Fatalf("expected the phone to be saved, got %d executions", uc.executed)
	}
	if !transport.hit("subscribed_apps") {
		t.Fatalf("expected the webhook subscription to run, got %v", transport.urls)
	}
}

func TestMetaOnboarding_LimitReachedAtSaveMapsTo403(t *testing.T) {
	uc := &fakeOnboardUC{executeErr: businessphone.ErrPhoneLimitReached}
	h, _ := newQuotaHandler(uc)

	rec := runMetaOnboarding(h)

	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "phone_limit_reached") {
		t.Fatalf("expected 403 phone_limit_reached, got %d: %s", rec.Code, rec.Body.String())
	}
}
