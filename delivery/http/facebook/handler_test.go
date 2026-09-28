package facebook

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	fbdomain "vozko/domain/facebook"
	fbuc "vozko/usecases/facebook"
	"vozko/usecases/shared/oauthstate"
)

type memoryKV struct{ values map[string]string }

func (m *memoryKV) SetNX(key, value string, _ time.Duration) (bool, error) {
	if _, ok := m.values[key]; ok {
		return false, nil
	}
	m.values[key] = value
	return true, nil
}
func (m *memoryKV) Exists(key string) (bool, error) { _, ok := m.values[key]; return ok, nil }
func (m *memoryKV) Del(keys ...string) error {
	for _, k := range keys {
		delete(m.values, k)
	}
	return nil
}

func callbackHandler(t *testing.T) *Handler {
	t.Helper()
	nonces, _ := oauthstate.NewNonceStore(&memoryKV{values: map[string]string{}}, "fb:oauth")
	issuer, err := oauthstate.NewIssuer("secret", nonces, "/dashboard/facebook-pages")
	if err != nil {
		t.Fatal(err)
	}
	return NewHandler(HandlerDeps{
		Connect:         fbuc.NewConnectPagesUseCase(nil, nil, nil, nil, nil, issuer),
		PictureURL:      func(string) string { return "" },
		FrontendBaseURL: "https://app.example",
	})
}

func TestCallbackWithABadStateRedirectsWithTheReason(t *testing.T) {
	rec := httptest.NewRecorder()
	callbackHandler(t).HandleCallback(rec, httptest.NewRequest(http.MethodGet, "/oauth/facebook/callback?code=c&state=forged.sig", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("code = %d", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc.Host != "app.example" || loc.Path != "/dashboard/facebook-pages" || loc.Query().Get("facebook") != "error" || loc.Query().Get("reason") != "invalid_state" {
		t.Fatalf("location = %s", loc)
	}
}

func TestConnectErrorCodes(t *testing.T) {
	cases := map[error]string{
		oauthstate.ErrInvalidState:                             "invalid_state",
		oauthstate.ErrReplayedState:                            "invalid_state",
		oauthstate.ErrExpiredState:                             "expired_state",
		fmt.Errorf("x: %w", fbdomain.ErrAuthorizationDenied):   "declined",
		&fbuc.ConnectError{Err: fbdomain.ErrNoPagesGranted}:    "no_pages_granted",
		&fbuc.ConnectError{Err: fbdomain.ErrGrantUnverifiable}: "grant_unverifiable",
		errors.New("boom"):                                     "connect_failed",
	}
	for err, want := range cases {
		if got := connectErrorCode(err); got != want {
			t.Errorf("%v -> %q, want %q", err, got, want)
		}
	}
}

func TestPresenterExposesCapabilitiesAndPictureURL(t *testing.T) {
	p := pagePresenter{pictureURL: func(k string) string { return "https://cdn/" + k }}
	page := &fbdomain.Page{
		ID: "p", Status: fbdomain.StatusConnected, PictureStorageKey: "k",
		Tasks: []fbdomain.Task{fbdomain.TaskMessaging}, GrantedScopes: []string{fbdomain.ScopeMessaging, fbdomain.ScopeManageMetadata},
	}
	out := p.page(page)
	if out.PictureURL != "https://cdn/k" || !out.Capabilities.Messaging || out.Capabilities.Publish {
		t.Fatalf("response = %+v", out)
	}
	if out.SubscribedFields == nil || out.GrantedScopes == nil {
		t.Fatal("list fields must serialise as arrays, not null")
	}
}

func TestInvalidPostCarriesItsReason(t *testing.T) {
	rec := httptest.NewRecorder()
	writeDomainError(rec, fmt.Errorf("%w: an album takes 2 to 10 images", fbdomain.ErrInvalidPost), "fallback")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "an album takes 2 to 10 images") || !strings.Contains(rec.Body.String(), "invalid_post") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteNotPermittedPointsToBusinessSuite(t *testing.T) {
	rec := httptest.NewRecorder()
	writeDomainError(rec, errors.Join(fbdomain.ErrDeleteNotPermitted, errors.New("(#10)")), "fallback")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "delete_not_permitted") || !strings.Contains(rec.Body.String(), fbdomain.ManagePostsURL) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestProfileAndReelLimitsAreTooManyRequests(t *testing.T) {
	for err, code := range map[error]string{fbdomain.ErrProfileRateLimited: "profile_rate_limited", fbdomain.ErrReelLimit: "reel_limit"} {
		rec := httptest.NewRecorder()
		writeDomainError(rec, err, "fallback")
		if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), code) {
			t.Errorf("%v -> %d %s", err, rec.Code, rec.Body.String())
		}
	}
}

func TestProfileJSONRoundTrip(t *testing.T) {
	body := ProfileBody{
		Greeting:       []ProfileText{{Locale: "default", Text: "Olá"}},
		GetStarted:     &ProfilePayload{Payload: "GO"},
		IceBreakers:    []ProfileIceBreakers{{Locale: "default", Items: []ProfileQuestion{{Question: "Q", Payload: "P"}}}},
		PersistentMenu: []ProfileMenu{{Locale: "default", ComposerInputDisabled: true, Items: []ProfileMenuItem{{Type: "web_url", Title: "Site", URL: "https://x"}}}},
	}
	back := presentProfile(body.toDomain())
	if back.Greeting[0].Text != "Olá" || back.GetStarted.Payload != "GO" || back.IceBreakers[0].Items[0].Payload != "P" ||
		!back.PersistentMenu[0].ComposerInputDisabled || back.PersistentMenu[0].Items[0].URL != "https://x" {
		t.Fatalf("round trip = %+v", back)
	}
}

func TestReelLimitFailureCarriesItsReason(t *testing.T) {
	job := &fbdomain.PublishJob{Request: fbdomain.PublishRequest{Kind: fbdomain.PublishReel}, Status: fbdomain.JobFailed, ErrorCode: fbdomain.CodeReelCap}
	if got := presentJob(job); got.Error == nil || got.Error.Reason != "reel_limit" {
		t.Fatalf("job = %+v", got.Error)
	}
}
