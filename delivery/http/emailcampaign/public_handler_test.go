package emailcampaignhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/emailconsent"
	emailconsent_usecase "vozko/usecases/emailconsent"
)

const goodToken = "abcdefghijklmnopqrstuvwxyz234567abcdefghij"

type fakeConsent struct {
	sources []emailconsent.UnsubscribeSource
}

func (f *fakeConsent) Subscribe(context.Context, string, emailconsent_usecase.SubscribeInput) (emailconsent.Subscription, error) {
	return emailconsent.Subscription{}, nil
}
func (f *fakeConsent) ImportConsent(context.Context, string, []emailconsent_usecase.ImportRow) (emailconsent_usecase.ImportResult, error) {
	return emailconsent_usecase.ImportResult{}, nil
}
func (f *fakeConsent) UnsubscribeAddress(context.Context, string, string) (emailconsent.Subscription, error) {
	return emailconsent.Subscription{}, nil
}
func (f *fakeConsent) CheckToken(token string) error {
	if token != goodToken {
		return emailconsent.ErrUnsubscribeTokenInvalid
	}
	return nil
}
func (f *fakeConsent) Unsubscribe(_ context.Context, token string, source emailconsent.UnsubscribeSource) (emailconsent_usecase.UnsubscribeResult, error) {
	if err := f.CheckToken(token); err != nil {
		return emailconsent_usecase.UnsubscribeResult{}, err
	}
	f.sources = append(f.sources, source)
	return emailconsent_usecase.UnsubscribeResult{}, nil
}

func publicRouter(c *fakeConsent) *mux.Router {
	r := mux.NewRouter()
	RegisterPublicRoutes(r, NewPublicHandler(c))
	return r
}

func serveRequest(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestTheConfirmationPageNeverUnsubscribes(t *testing.T) {
	c := &fakeConsent{}
	rec := serveRequest(publicRouter(c), http.MethodGet, "/public/email/unsubscribe/"+goodToken, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `action="/public/email/unsubscribe/`+goodToken+`"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if len(c.sources) != 0 {
		t.Fatal("a GET must not unsubscribe")
	}
	if rec.Header().Get("X-Robots-Tag") != "noindex" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("the page is not indexed or cached")
	}
	bad := serveRequest(publicRouter(c), http.MethodGet, "/public/email/unsubscribe/"+strings.Repeat("a", 42), "")
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "Link inválido") {
		t.Fatalf("invalid token page = %d", bad.Code)
	}
}

func TestOneClickAndConfirmedUnsubscribe(t *testing.T) {
	c := &fakeConsent{}
	r := publicRouter(c)
	rec := serveRequest(r, http.MethodPost, "/public/email/unsubscribe/"+goodToken, "List-Unsubscribe=One-Click")
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("one click = %d %q", rec.Code, rec.Body.String())
	}
	rec = serveRequest(r, http.MethodPost, "/public/email/unsubscribe/"+goodToken, "confirm=link")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "descadastrado") {
		t.Fatalf("confirmed = %d", rec.Code)
	}
	if len(c.sources) != 2 || c.sources[0] != emailconsent.UnsubscribeOneClick || c.sources[1] != emailconsent.UnsubscribeLink {
		t.Fatalf("sources = %v", c.sources)
	}
	rec = serveRequest(r, http.MethodPost, "/public/email/unsubscribe/"+strings.Repeat("b", 42), "List-Unsubscribe=One-Click")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), CodeTokenInvalid) {
		t.Fatalf("invalid one click = %d %s", rec.Code, rec.Body.String())
	}
	rec = serveRequest(r, http.MethodPost, "/public/email/unsubscribe/"+goodToken, "anything")
	if rec.Code != http.StatusBadRequest || len(c.sources) != 2 {
		t.Fatal("a post without one click or confirmation changes nothing")
	}
}

func TestPreviewLandingIsHarmless(t *testing.T) {
	rec := serveRequest(publicRouter(&fakeConsent{}), http.MethodGet, "/public/email/preview", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Pré-visualização") {
		t.Fatalf("preview = %d", rec.Code)
	}
}
