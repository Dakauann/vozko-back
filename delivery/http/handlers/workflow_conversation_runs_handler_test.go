package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type conversationRunsStub struct {
	viewer                          shared.Person
	workspaceID, entryType, entryID string
	page                            shared.Pagination
	err                             error
}

func (s *conversationRunsStub) Execute(viewer shared.Person, workspaceID, entryType, entryID string, page shared.Pagination) (*shared.PaginatedResult[*workflow.ConversationRun], error) {
	s.viewer, s.workspaceID, s.entryType, s.entryID, s.page = viewer, workspaceID, entryType, entryID, page
	if s.err != nil {
		return nil, s.err
	}
	version, duration := 3, int64(1500)
	started := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	items := []*workflow.ConversationRun{{
		ID: "run-1", WorkflowID: "wf-1", WorkflowName: "Matrícula", WorkflowVersion: &version, EntryID: entryID, EntryType: entryType,
		Status: workflow.RunStatusCompleted, TriggerType: workflow.TriggerWebhook, StartedBy: "webhook:d-1", StartedAt: started, DurationMs: &duration,
	}}
	return shared.NewPaginatedResult(items, shared.NormalizePagination(page), 1), nil
}

func conversationRunsRequest(claims *auth.Claims) *http.Request {
	r := workflowRequestAs(http.MethodGet, "/conversations/whatsapp/e-1/workflow-runs?page=2&pageSize=10", nil, claims)
	return mux.SetURLVars(r, map[string]string{"entryType": "whatsapp", "entryId": "e-1"})
}

func TestTheConversationRunListAsksForTheViewersConversation(t *testing.T) {
	stub := &conversationRunsStub{}
	rec := httptest.NewRecorder()
	(&WorkflowHandler{conversationRunsUC: stub}).ListConversationRuns(rec, conversationRunsRequest(&auth.Claims{UserID: "u1"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if stub.viewer.UserID != "u1" || stub.workspaceID != "ws1" || stub.entryType != "whatsapp" || stub.entryID != "e-1" || stub.page.Page != 2 || stub.page.PageSize != 10 {
		t.Fatalf("use case got %+v", stub)
	}
	var body struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Data) != 1 {
		t.Fatalf("body = %s", rec.Body.String())
	}
	got := body.Data[0]
	if got["workflowName"] != "Matrícula" || got["workflowVersion"] != float64(3) || got["durationMs"] != float64(1500) || got["startedBy"] != "webhook:d-1" {
		t.Fatalf("item = %v", got)
	}
}

func TestAConversationTheViewerCannotSeeReadsAsNotFound(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		code int
	}{
		"hidden":     {conversation.ErrNotVisible, http.StatusNotFound},
		"unreadable": {errors.New("db down"), http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			(&WorkflowHandler{conversationRunsUC: &conversationRunsStub{err: tc.err}}).ListConversationRuns(rec, conversationRunsRequest(&auth.Claims{UserID: "u1"}))
			if rec.Code != tc.code {
				t.Fatalf("status = %d", rec.Code)
			}
		})
	}
}

func TestTheConversationRunListNeedsAnAuthenticatedViewer(t *testing.T) {
	stub := &conversationRunsStub{}
	rec := httptest.NewRecorder()
	(&WorkflowHandler{conversationRunsUC: stub}).ListConversationRuns(rec, conversationRunsRequest(nil))
	if rec.Code != http.StatusUnauthorized || stub.workspaceID != "" {
		t.Fatalf("status = %d, listed for %q", rec.Code, stub.workspaceID)
	}
}
