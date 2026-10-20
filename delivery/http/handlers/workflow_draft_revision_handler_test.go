package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/workflow"
	workflow_usecase "vozko/usecases/workflow"
)

type revisionSavingWorkflows struct {
	workflow.ScopedWorkflowsUseCase
	revision int
	called   bool
}

func (f *revisionSavingWorkflows) save(id string, revision int) (*workflow.Workflow, error) {
	f.called, f.revision = true, revision
	if err := workflow.RequireDraftRevision(revision); err != nil {
		return nil, err
	}
	if revision != 4 {
		return nil, &workflow.DraftConflict{WorkflowID: id, Expected: revision, Current: 7}
	}
	return &workflow.Workflow{ID: id, Version: revision + 1}, nil
}

func (f *revisionSavingWorkflows) Update(_ context.Context, _ workflow.Scope, id string, _ *workflow.Workflow, revision int) (*workflow.Workflow, error) {
	return f.save(id, revision)
}

func (f *revisionSavingWorkflows) RestoreVersion(_ context.Context, _ workflow.Scope, id string, _, revision int) (*workflow.Workflow, error) {
	return f.save(id, revision)
}

type savedAnswer struct {
	Code    string `json:"code"`
	Version int    `json:"version"`
	Details struct {
		Revision int `json:"revision"`
	} `json:"details"`
}

func answerOf(t *testing.T, rec *httptest.ResponseRecorder) savedAnswer {
	t.Helper()
	var got savedAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return got
}

func TestSavingTheDraftSendsTheRevisionItEditedAndAnswersTheNextOne(t *testing.T) {
	workflows := &revisionSavingWorkflows{}
	rec := httptest.NewRecorder()
	(&WorkflowHandler{workflows: workflows}).Update(rec, workflowRequestAs(http.MethodPut, "/workflows/w1", []byte(`{"name":"Fluxo","revision":4}`), &auth.Claims{UserID: "u1"}))

	if rec.Code != http.StatusOK || workflows.revision != 4 {
		t.Fatalf("status = %d revision sent = %d body = %s", rec.Code, workflows.revision, rec.Body.String())
	}
	if got := answerOf(t, rec); got.Version != 5 {
		t.Fatalf("version = %d, want the next revision 5", got.Version)
	}
}

func TestEveryDraftWriterRefusesARequestWithoutTheRevision(t *testing.T) {
	cases := []struct {
		name  string
		serve func(h *WorkflowHandler, w http.ResponseWriter)
	}{
		{"save", func(h *WorkflowHandler, w http.ResponseWriter) {
			h.Update(w, workflowRequestAs(http.MethodPut, "/workflows/w1", []byte(`{"name":"Fluxo"}`), &auth.Claims{UserID: "u1"}))
		}},
		{"save at revision 0", func(h *WorkflowHandler, w http.ResponseWriter) {
			h.Update(w, workflowRequestAs(http.MethodPut, "/workflows/w1", []byte(`{"name":"Fluxo","revision":0}`), &auth.Claims{UserID: "u1"}))
		}},
		{"restore", func(h *WorkflowHandler, w http.ResponseWriter) {
			h.RestoreVersion(w, restoreRequest(`{}`))
		}},
		{"pin from run", func(h *WorkflowHandler, w http.ResponseWriter) {
			h.PinFromRun(w, pinRequest(`{"runId":"r-9"}`))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			pins := &pinnerSpy{}
			tc.serve(&WorkflowHandler{workflows: &revisionSavingWorkflows{}, nodePins: pins}, rec)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body = %s, want 400 (fail closed, never skip the check)", rec.Code, rec.Body.String())
			}
			if got := answerOf(t, rec); got.Code != draftRevisionRequiredCode {
				t.Fatalf("code = %q", got.Code)
			}
		})
	}
}

func TestASaveOverANewerDraftAnswersTheConflictWithTheStoredRevision(t *testing.T) {
	cases := []struct {
		name  string
		serve func(h *WorkflowHandler, w http.ResponseWriter)
	}{
		{"save", func(h *WorkflowHandler, w http.ResponseWriter) {
			h.Update(w, workflowRequestAs(http.MethodPut, "/workflows/w1", []byte(`{"name":"Fluxo","revision":3}`), &auth.Claims{UserID: "u1"}))
		}},
		{"restore", func(h *WorkflowHandler, w http.ResponseWriter) {
			h.RestoreVersion(w, restoreRequest(`{"revision":3}`))
		}},
		{"pin from run", func(h *WorkflowHandler, w http.ResponseWriter) {
			h.PinFromRun(w, pinRequest(`{"runId":"r-9","revision":3}`))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			pins := &pinnerSpy{err: &workflow.DraftConflict{WorkflowID: "w1", Expected: 3, Current: 7}}
			tc.serve(&WorkflowHandler{workflows: &revisionSavingWorkflows{}, nodePins: pins}, rec)
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
			}
			if got := answerOf(t, rec); got.Code != workflow.DraftConflictCode || got.Details.Revision != 7 {
				t.Fatalf("answer = %+v, want draft_conflict naming revision 7", got)
			}
		})
	}
}

func TestRestoreAndPinSendTheRevisionTheyEdited(t *testing.T) {
	workflows := &revisionSavingWorkflows{}
	rec := httptest.NewRecorder()
	(&WorkflowHandler{workflows: workflows}).RestoreVersion(rec, restoreRequest(`{"revision":4}`))
	if rec.Code != http.StatusOK || workflows.revision != 4 {
		t.Fatalf("restore status = %d revision = %d", rec.Code, workflows.revision)
	}

	pins := &pinnerSpy{}
	rec = httptest.NewRecorder()
	(&WorkflowHandler{nodePins: pins}).PinFromRun(rec, pinRequest(`{"runId":"r-9","revision":4}`))
	if rec.Code != http.StatusOK || pins.in != (workflow_usecase.PinFromRunInput{WorkflowID: "w1", NodeID: "n55", RunID: "r-9", Revision: 4}) {
		t.Fatalf("pin status = %d in = %+v", rec.Code, pins.in)
	}
}

func restoreRequest(body string) *http.Request {
	r := workflowRequestAs(http.MethodPost, "/workflows/w1/versions/2/restore", []byte(body), &auth.Claims{UserID: "u1"})
	return mux.SetURLVars(r, map[string]string{"id": "w1", "version": "2"})
}
