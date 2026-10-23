package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/delivery/http/response/responsetest"
	"vozko/domain/auth"
	conversationdomain "vozko/domain/conversation"
	em "vozko/domain/emailinbox"
	"vozko/infra/http/middleware"
	"vozko/pkg/codegolden"
	emailinbox_usecase "vozko/usecases/emailinbox"
)

type stubEmailHTML struct {
	got  emailinbox_usecase.HTMLRequest
	view em.HTMLView
	err  error
}

func (s *stubEmailHTML) View(_ context.Context, in emailinbox_usecase.HTMLRequest) (em.HTMLView, error) {
	s.got = in
	return s.view, s.err
}

func emailHTMLRequest(entryType, query string, withCaller bool) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/conversations/"+entryType+"/conv-1/messages/m-1/email-html"+query, nil)
	r = mux.SetURLVars(r, map[string]string{"entryType": entryType, "entryId": "conv-1", "messageId": "m-1"})
	ctx := context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	if withCaller {
		ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "bob", Role: "user"})
	}
	return r.WithContext(ctx)
}

func TestTheEmailHTMLIsAskedForTheCallerTheConversationAndTheImageChoice(t *testing.T) {
	stub := &stubEmailHTML{view: em.HTMLView{HTML: "<p>oi</p>", HasHTML: true, Inbound: true, ImagesBlocked: true}}
	h := &ConversationHandler{}
	h.SetEmailHTML(stub)
	rec := httptest.NewRecorder()
	h.GetEmailHTML(rec, emailHTMLRequest("email", "?images=show", true))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if stub.got.Viewer.UserID != "bob" || stub.got.WorkspaceID != "ws-1" || stub.got.EntryID != "conv-1" || stub.got.MessageID != "m-1" || !stub.got.ShowImages {
		t.Fatalf("use case got %+v", stub.got)
	}
	var body em.HTMLView
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || !body.HasHTML || !body.ImagesBlocked || body.HTML != "<p>oi</p>" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	h.GetEmailHTML(httptest.NewRecorder(), emailHTMLRequest("email", "", true))
	if stub.got.ShowImages {
		t.Fatal("remote images stay blocked unless asked")
	}
}

func TestEveryEmailHTMLRefusalWritesItsStableCode(t *testing.T) {
	responsetest.RequireStableCodes(t, emailHTMLRefusals)
	with := func(err error) func() *ConversationHandler {
		return func() *ConversationHandler {
			h := &ConversationHandler{}
			h.SetEmailHTML(&stubEmailHTML{err: err})
			return h
		}
	}
	tests := []struct {
		name string
		h    func() *ConversationHandler
		r    *http.Request
		want response.Refusal
	}{
		{"no viewer", func() *ConversationHandler { return &ConversationHandler{} }, emailHTMLRequest("email", "", true), refusalEmailHTMLUnavailable},
		{"no caller", with(nil), emailHTMLRequest("email", "", false), refusalEmailHTMLUnauthorized},
		{"a conversation outside the caller's scope", with(conversationdomain.ErrNotVisible), emailHTMLRequest("email", "", true), refusalEmailHTMLHidden},
		{"a message of another conversation", with(em.ErrNotAnEmail), emailHTMLRequest("email", "", true), refusalEmailHTMLNotAnEmail},
		{"another channel", with(nil), emailHTMLRequest("whatsapp", "", true), refusalEmailHTMLNotAnEmail},
		{"an unreadable message", with(errors.New("db down")), emailHTMLRequest("email", "", true), refusalEmailHTMLFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.h().GetEmailHTML(rec, tc.r)
			if rec.Code != tc.want.Status || responsetest.CodeOf(t, rec) != tc.want.Code {
				t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestEmailHTMLErrorCodesMatchTheGoldenSoTheFrontTranslatesThem(t *testing.T) {
	codegolden.Compare(t, filepath.Join("testdata", "email_html_error_codes.golden"), response.RefusalCodes(emailHTMLRefusals),
		"update testdata/email_html_error_codes.golden and translate crmConversation.emailView.errors.<code> in the pt, en, es and de catalogs of vozko-front")
}
