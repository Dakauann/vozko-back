package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	aa "vozko/domain/ai_attendance"
	"vozko/domain/auth"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
	aa_usecase "vozko/usecases/ai_attendance"
)

type stubToolCalls struct {
	person                          shared.Person
	workspaceID, entryType, entryID string
	page                            shared.Pagination
	err                             error
}

func (s *stubToolCalls) Execute(person shared.Person, workspaceID, entryType, entryID string, page shared.Pagination) (*shared.PaginatedResult[*aa.ToolCall], error) {
	s.person, s.workspaceID, s.entryType, s.entryID, s.page = person, workspaceID, entryType, entryID, page
	if s.err != nil {
		return nil, s.err
	}
	return shared.NewPaginatedResult([]*aa.ToolCall{{ID: "c-1", ToolName: "http_request", Outcome: aa.ToolCallError, Error: "409"}}, page, 1), nil
}

func toolCallsRequest(role string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/conversations/whatsapp/entry-1/ai-tool-calls?page=1&pageSize=5", nil)
	r = mux.SetURLVars(r, map[string]string{"entryType": "whatsapp", "entryId": "entry-1"})
	ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "bob", Role: role})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	return r.WithContext(ctx)
}

func TestToolCallsAreListedForTheCallerAndTheConversation(t *testing.T) {
	stub := &stubToolCalls{}
	h := &ConversationHandler{}
	h.SetToolCalls(stub)
	w := httptest.NewRecorder()
	h.ListAIToolCalls(w, toolCallsRequest("user"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if stub.person.UserID != "bob" || stub.person.SystemAdmin || stub.workspaceID != "ws-1" || stub.entryType != "whatsapp" || stub.entryID != "entry-1" || stub.page.PageSize != 5 {
		t.Fatalf("use case got %+v", stub)
	}
	var body struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Data) != 1 || body.Data[0]["toolName"] != "http_request" {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestToolCallsOfAnInvisibleConversationAreNotFound(t *testing.T) {
	h := &ConversationHandler{}
	h.SetToolCalls(&stubToolCalls{err: aa_usecase.ErrConversationNotVisible})
	w := httptest.NewRecorder()
	h.ListAIToolCalls(w, toolCallsRequest("user"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d", w.Code)
	}

	h.SetToolCalls(&stubToolCalls{err: errors.New("db down")})
	w = httptest.NewRecorder()
	h.ListAIToolCalls(w, toolCallsRequest("user"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestToolCallsWithoutTheServiceAreUnavailable(t *testing.T) {
	w := httptest.NewRecorder()
	(&ConversationHandler{}).ListAIToolCalls(w, toolCallsRequest("user"))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", w.Code)
	}
}
