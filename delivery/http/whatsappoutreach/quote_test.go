package whatsappoutreach

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/conversation"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/infra/http/middleware"
)

type failingQuote struct{ err error }

func (q failingQuote) Execute(context.Context, string, string, string) (*wo.SendQuote, error) {
	return nil, q.err
}

type openScope struct{}

func (openScope) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, true
}

func quoteRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/whatsapp/outreach/quote?templateId=t1", nil)
	ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u1"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws1")
	return r.WithContext(ctx)
}

func TestQuoteAnswersUnavailableWhenTheGrantsCannotBeRead(t *testing.T) {
	h := NewHandler(HandlerDeps{Quote: failingQuote{err: errors.New("boom")}, Departments: openScope{}})
	rec := httptest.NewRecorder()
	h.Quote(rec, quoteRequest())
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"quote_unavailable"`) || strings.Contains(rec.Body.String(), "enviar") {
		t.Fatalf("status = %d body = %s, want 503 quote_unavailable", rec.Code, rec.Body.String())
	}
}

func TestQuoteKeepsTheCodeOfARefusal(t *testing.T) {
	h := NewHandler(HandlerDeps{Quote: failingQuote{err: wo.ErrTemplateForbidden}, Departments: openScope{}})
	rec := httptest.NewRecorder()
	h.Quote(rec, quoteRequest())
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"forbidden"`) {
		t.Fatalf("status = %d body = %s, want 403 forbidden", rec.Code, rec.Body.String())
	}
}
