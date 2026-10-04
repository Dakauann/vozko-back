package conversation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/conversation"
	"vozko/infra/http/middleware"
	ia_usecase "vozko/usecases/inbox_assignment"
)

type stubDelegation struct {
	got ia_usecase.DelegateInput
	res ia_usecase.OperatorAutomationResult
	err error
}

func (s *stubDelegation) Delegate(_ context.Context, in ia_usecase.DelegateInput) (ia_usecase.OperatorAutomationResult, error) {
	s.got = in
	return s.res, s.err
}

func delegationRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/conversations/webchat/entry-1/delegation", strings.NewReader(body))
	r = mux.SetURLVars(r, map[string]string{"entryType": "webchat", "entryId": "entry-1"})
	ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "bob"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	return r.WithContext(ctx)
}

func TestDelegatePassesTheCallerAndTheChosenAutomation(t *testing.T) {
	svc := &stubDelegation{res: ia_usecase.OperatorAutomationResult{Owner: "workflow:wf-9"}}
	h := &ConversationHandler{}
	h.SetDelegationService(svc)

	w := httptest.NewRecorder()
	h.Delegate(w, delegationRequest(`{"kind":"workflow","id":"wf-9"}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	want := conversation.Automation{Kind: conversation.AutomationWorkflow, ID: "wf-9"}
	if svc.got.ActorUserID != "bob" || svc.got.WorkspaceID != "ws-1" || svc.got.EntryID != "entry-1" || svc.got.EntryType != "webchat" || svc.got.Automation != want {
		t.Fatalf("input = %+v", svc.got)
	}
	var body map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["assigned_user_id"] != "workflow:wf-9" {
		t.Fatalf("assigned_user_id = %v", body["assigned_user_id"])
	}
}

func TestDelegateMapsRefusals(t *testing.T) {
	cases := map[string]struct {
		body string
		err  error
		want int
		code string
	}{
		"not an automation": {`{"kind":"person","id":"bob"}`, nil, http.StatusBadRequest, "automation_invalid"},
		"no access":         {`{"kind":"agent","id":"a"}`, ia_usecase.ErrAutomationForbidden, http.StatusForbidden, ""},
		"unusable":          {`{"kind":"agent","id":"a"}`, conversation.ErrAutomationUnusable, http.StatusUnprocessableEntity, "automation_unusable"},
		"nothing answers":   {`{"kind":"agent","id":"a"}`, ia_usecase.ErrNothingToReturnTo, http.StatusConflict, "nothing_to_return_to"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := &ConversationHandler{}
			h.SetDelegationService(&stubDelegation{err: tc.err})

			w := httptest.NewRecorder()
			h.Delegate(w, delegationRequest(tc.body))

			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", w.Code, tc.want, w.Body.String())
			}
			if tc.code != "" {
				var body map[string]interface{}
				_ = json.Unmarshal(w.Body.Bytes(), &body)
				if body["code"] != tc.code {
					t.Fatalf("code = %v, want %s", body["code"], tc.code)
				}
			}
		})
	}
}
