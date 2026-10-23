package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type recordingWorkflowList struct {
	input *workflow.ListWorkflowsInput
}

func (s *recordingWorkflowList) Execute(in workflow.ListWorkflowsInput) (*shared.PaginatedResult[*workflow.Workflow], error) {
	s.input = &in
	return shared.NewPaginatedResult([]*workflow.Workflow{}, shared.Pagination{Page: 1, PageSize: 20}, 0), nil
}

func TestTheWorkflowListFiltersByEntryKindAcrossPages(t *testing.T) {
	cases := []struct {
		query string
		code  int
		kind  workflow.EntryPointKind
	}{
		{"", http.StatusOK, ""},
		{"?entryKind=webhook", http.StatusOK, workflow.EntryWebhook},
		{"?entryKind=lead_exit&page=2", http.StatusOK, workflow.EntryLeadExit},
		{"?entryKind=trigger_webhook", http.StatusBadRequest, ""},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			list := &recordingWorkflowList{}
			h := &WorkflowHandler{listUC: list}
			rec := httptest.NewRecorder()
			h.List(rec, workflowRequestAs(http.MethodGet, "/workflows"+tc.query, nil, &auth.Claims{UserID: "u1"}))
			if rec.Code != tc.code {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
			}
			if tc.code == http.StatusBadRequest {
				if list.input != nil || !strings.Contains(rec.Body.String(), invalidEntryKindCode) {
					t.Fatalf("a bad kind must be refused before listing: %s", rec.Body.String())
				}
				return
			}
			got := workflow.EntryPointKind("")
			if list.input.EntryKind != nil {
				got = *list.input.EntryKind
			}
			if got != tc.kind {
				t.Fatalf("entry kind = %q, want %q", got, tc.kind)
			}
		})
	}
}

func TestTheWorkflowListOffersOnlyWhatAButtonCanStartWhenAsked(t *testing.T) {
	cases := []struct {
		query string
		code  int
		want  bool
	}{
		{"", http.StatusOK, false},
		{"?startsFromMessage=true", http.StatusOK, true},
		{"?startsFromMessage=false", http.StatusOK, false},
		{"?startsFromMessage=talvez", http.StatusBadRequest, false},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			list := &recordingWorkflowList{}
			h := &WorkflowHandler{listUC: list}
			rec := httptest.NewRecorder()
			h.List(rec, workflowRequestAs(http.MethodGet, "/workflows"+tc.query, nil, &auth.Claims{UserID: "u1"}))
			if rec.Code != tc.code {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
			}
			if tc.code == http.StatusBadRequest {
				if list.input != nil || !strings.Contains(rec.Body.String(), invalidFilterCode) {
					t.Fatalf("an unreadable filter must be refused before listing: %s", rec.Body.String())
				}
				return
			}
			if list.input.StartsFromMessage != tc.want {
				t.Fatalf("startsFromMessage = %v, want %v", list.input.StartsFromMessage, tc.want)
			}
		})
	}
}
