package workflowwebhook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/infra/http/middleware"
	"vozko/pkg/webhookanswer"
)

type stubDeliveries struct {
	viewer                  shared.Person
	workspaceID, workflowID string
	page                    shared.Pagination
	err                     error
}

func (s *stubDeliveries) Execute(viewer shared.Person, workspaceID, workflowID string, page shared.Pagination) (*shared.PaginatedResult[*workflow.WebhookDelivery], error) {
	s.viewer, s.workspaceID, s.workflowID, s.page = viewer, workspaceID, workflowID, page
	if s.err != nil {
		return nil, s.err
	}
	items := []*workflow.WebhookDelivery{{ID: "d-1", Outcome: workflow.WebhookOutcomeRejected, Reason: "workflow_not_active", HTTPStatus: 409}}
	return shared.NewPaginatedResult(items, page, 1), nil
}

type scopedAccess struct {
	err   error
	asked string
}

func (s *scopedAccess) Get(scope workflow.Scope, id string) (*workflow.Workflow, error) {
	s.asked = scope.WorkspaceID + "/" + id
	if s.err != nil {
		return nil, s.err
	}
	return &workflow.Workflow{ID: id, WorkspaceID: scope.WorkspaceID}, nil
}

func TestDeliveriesOfAnotherDepartmentsWorkflowAreRefusedBeforeListing(t *testing.T) {
	cases := []struct {
		name   string
		access *scopedAccess
		h      func(*scopedAccess, *stubDeliveries) *Handler
		want   int
	}{
		{"another department", &scopedAccess{err: workflow.ErrWorkflowAccessDenied}, func(a *scopedAccess, s *stubDeliveries) *Handler {
			return &Handler{deliveriesUC: s, workflows: a}
		}, http.StatusForbidden},
		{"another workspace", &scopedAccess{err: workflow.ErrWorkflowNotFound}, func(a *scopedAccess, s *stubDeliveries) *Handler {
			return &Handler{deliveriesUC: s, workflows: a}
		}, http.StatusNotFound},
		{"no scoped access wired", nil, func(_ *scopedAccess, s *stubDeliveries) *Handler {
			return &Handler{deliveriesUC: s}
		}, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubDeliveries{}
			rec := httptest.NewRecorder()
			tc.h(tc.access, stub).ListWorkflowWebhookDeliveries(rec, deliveriesReq(""))
			if rec.Code != tc.want || stub.workspaceID != "" {
				t.Fatalf("status = %d, listed for %q; deliveries without a conversation must follow the workflow's department", rec.Code, stub.workspaceID)
			}
		})
	}
}

func deliveriesReq(query string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/workflows/wf-1/webhook/deliveries"+query, nil)
	r = mux.SetURLVars(r, map[string]string{"id": "wf-1"})
	ctx := context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	return r.WithContext(context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1"}))
}

func TestDeliveriesAreListedForTheViewerSoConversationsFollowTheInboxRule(t *testing.T) {
	stub := &stubDeliveries{}
	(&Handler{deliveriesUC: stub, workflows: &scopedAccess{}}).ListWorkflowWebhookDeliveries(httptest.NewRecorder(), deliveriesReq(""))
	if stub.viewer.UserID != "user-1" {
		t.Fatalf("viewer = %+v", stub.viewer)
	}
}

func TestDeliveriesNeedAnAuthenticatedViewer(t *testing.T) {
	stub := &stubDeliveries{}
	r := httptest.NewRequest(http.MethodGet, "/workflows/wf-1/webhook/deliveries", nil)
	r = r.WithContext(context.WithValue(mux.SetURLVars(r, map[string]string{"id": "wf-1"}).Context(), middleware.WorkspaceIDContextKey, "ws-1"))
	rec := httptest.NewRecorder()
	(&Handler{deliveriesUC: stub, workflows: &scopedAccess{}}).ListWorkflowWebhookDeliveries(rec, r)
	if rec.Code != http.StatusUnauthorized || stub.workspaceID != "" {
		t.Fatalf("status = %d, listed for %q", rec.Code, stub.workspaceID)
	}
}

func TestDeliveriesAreListedForTheCallersWorkspaceAndPage(t *testing.T) {
	stub := &stubDeliveries{}
	h := &Handler{deliveriesUC: stub, workflows: &scopedAccess{}}
	rec := httptest.NewRecorder()
	h.ListWorkflowWebhookDeliveries(rec, deliveriesReq("?page=2&pageSize=10"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if stub.workspaceID != "ws-1" || stub.workflowID != "wf-1" || stub.page.Page != 2 || stub.page.PageSize != 10 {
		t.Fatalf("use case got %+v", stub)
	}
	var body struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Data) != 1 || body.Data[0]["reason"] != "workflow_not_active" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestDeliveriesFailClosedOnAnUnreadableLog(t *testing.T) {
	h := &Handler{deliveriesUC: &stubDeliveries{err: errors.New("db down")}, workflows: &scopedAccess{}}
	rec := httptest.NewRecorder()
	h.ListWorkflowWebhookDeliveries(rec, deliveriesReq(""))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTriggerErrorsAnswerWithTheStatusTheDeliveryLogRecords(t *testing.T) {
	for _, err := range []error{
		workflow.ErrWebhookNotFound, workflow.ErrWebhookInactive, workflow.ErrWebhookMethodNotAllowed, workflow.ErrWebhookUnauthorized,
		workflow.ErrWebhookEntryRequired, workflow.ErrWebhookEntryNotFound, workflow.ErrWebhookEntryForbidden,
		workflow.ErrWebhookNoTriggerNode, workflow.ErrWorkflowNotActive, workflow.ErrWorkspaceAtCapacity, errors.New("other"),
	} {
		rec := httptest.NewRecorder()
		handleWebhookTriggerError(rec, err)
		if want := webhookanswer.RejectionStatus(workflow.WebhookRejectionReason(err)); rec.Code != want {
			t.Errorf("%v answered %d, the delivery log says %d", err, rec.Code, want)
		}
	}
}
