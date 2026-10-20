package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type publishingWorkflows struct {
	workflow.ScopedWorkflowsUseCase
	publishErr error
	restored   int
	scope      workflow.Scope
}

func (f *publishingWorkflows) Publish(_ context.Context, scope workflow.Scope, id string) (*workflow.Workflow, error) {
	f.scope = scope
	if f.publishErr != nil {
		return nil, f.publishErr
	}
	one := 1
	return &workflow.Workflow{ID: id, Status: workflow.WorkflowStatusActive, PublishedVersion: &one}, nil
}

func (f *publishingWorkflows) Versions(_ workflow.Scope, id string, _ shared.QueryOptions) (*shared.PaginatedResult[*workflow.WorkflowVersion], error) {
	return &shared.PaginatedResult[*workflow.WorkflowVersion]{Items: []*workflow.WorkflowVersion{{WorkflowID: id, Version: 2}, {WorkflowID: id, Version: 1}}, Page: 1, PageSize: 20, TotalItems: 2, TotalPages: 1}, nil
}

func (f *publishingWorkflows) RestoreVersion(_ context.Context, _ workflow.Scope, id string, version, _ int) (*workflow.Workflow, error) {
	f.restored = version
	if version == 9 {
		return nil, fmt.Errorf("%w: workflow %s version %d", workflow.ErrVersionNotFound, id, version)
	}
	return &workflow.Workflow{ID: id, HasUnpublishedChanges: true}, nil
}

func TestPublishAnswersThePublishedWorkflowWithNothingLeftToPublish(t *testing.T) {
	workflows := &publishingWorkflows{}
	rec := httptest.NewRecorder()
	(&WorkflowHandler{workflows: workflows}).Publish(rec, workflowRequestAs(http.MethodPost, "/workflows/w1/publish", nil, &auth.Claims{UserID: "u1", Role: "admin"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["publishedVersion"] != float64(1) || resp["hasUnpublishedChanges"] != false {
		t.Fatalf("body = %v", resp)
	}
	if workflows.scope.Requester.UserID != "u1" {
		t.Fatalf("publisher = %q, want the caller", workflows.scope.Requester.UserID)
	}
}

func TestPublishErrorsMapToTheirStatus(t *testing.T) {
	cases := map[error]int{
		workflow.ErrVersionNumberTaken: http.StatusConflict,
		workflow.ErrCannotActivate:     http.StatusConflict,
		workflow.ErrGraphNoEndNode:     http.StatusUnprocessableEntity,
		workflow.ErrWorkflowNotFound:   http.StatusNotFound,
	}
	for err, want := range cases {
		rec := httptest.NewRecorder()
		(&WorkflowHandler{workflows: &publishingWorkflows{publishErr: err}}).Publish(rec, workflowRequestAs(http.MethodPost, "/workflows/w1/publish", nil, &auth.Claims{UserID: "u1"}))
		if rec.Code != want {
			t.Errorf("%v: status = %d, want %d", err, rec.Code, want)
		}
	}
}

func TestVersionsAreListedAsAPage(t *testing.T) {
	rec := httptest.NewRecorder()
	(&WorkflowHandler{workflows: &publishingWorkflows{}}).ListVersions(rec, workflowRequestAs(http.MethodGet, "/workflows/w1/versions", nil, &auth.Claims{UserID: "u1"}))
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusOK || len(resp.Data) != 2 || resp.Data[0]["version"] != float64(2) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestRestoreReadsTheVersionFromThePath(t *testing.T) {
	cases := []struct {
		version string
		want    int
	}{
		{"2", http.StatusOK},
		{"abc", http.StatusBadRequest},
		{"9", http.StatusNotFound},
	}
	for _, tc := range cases {
		workflows := &publishingWorkflows{}
		r := workflowRequestAs(http.MethodPost, "/workflows/w1/versions/"+tc.version+"/restore", []byte(`{"revision":1}`), &auth.Claims{UserID: "u1"})
		r = mux.SetURLVars(r, map[string]string{"id": "w1", "version": tc.version})
		rec := httptest.NewRecorder()
		(&WorkflowHandler{workflows: workflows}).RestoreVersion(rec, r)
		if rec.Code != tc.want {
			t.Errorf("version %s: status = %d, want %d", tc.version, rec.Code, tc.want)
		}
	}
}
