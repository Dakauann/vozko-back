package workflowwebhook

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/workflow"
	workflow_usecase "vozko/usecases/workflow"

	"github.com/gorilla/mux"
)

type stubTriggerUC struct {
	res *workflow_usecase.WebhookResult
	err error
}

func (s *stubTriggerUC) Execute(workflow_usecase.WebhookRequest) (*workflow_usecase.WebhookResult, error) {
	return s.res, s.err
}

type stubConfigUC struct {
	wh      *workflow.WorkflowWebhook
	err     error
	deleted bool
}

func (s *stubConfigUC) Get(string, string) (*workflow.WorkflowWebhook, error) { return s.wh, s.err }
func (s *stubConfigUC) Create(workflow_usecase.WebhookConfigInput) (*workflow.WorkflowWebhook, error) {
	return s.wh, s.err
}
func (s *stubConfigUC) Update(workflow_usecase.WebhookConfigInput) (*workflow.WorkflowWebhook, error) {
	return s.wh, s.err
}
func (s *stubConfigUC) Rotate(string, string) (*workflow.WorkflowWebhook, error) { return s.wh, s.err }
func (s *stubConfigUC) Delete(string, string) error {
	s.deleted = true
	return s.err
}

func triggerReq(token string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/webhooks/workflow/"+token, strings.NewReader(`{"entry_id":"e1"}`))
	return mux.SetURLVars(r, map[string]string{"token": token})
}

func TestHandleWebhookTrigger_Outcomes(t *testing.T) {
	cases := []struct {
		name string
		res  *workflow_usecase.WebhookResult
		want int
	}{
		{"accepted", &workflow_usecase.WebhookResult{RunID: "r1"}, http.StatusAccepted},
		{"duplicate", &workflow_usecase.WebhookResult{Duplicate: true}, http.StatusOK},
		{"already", &workflow_usecase.WebhookResult{AlreadyRunning: true, RunID: "r1"}, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := &Handler{triggerUC: &stubTriggerUC{res: c.res}}
			rec := httptest.NewRecorder()
			h.HandleWebhookTrigger(rec, triggerReq("tok"))
			if rec.Code != c.want {
				t.Fatalf("got %d want %d", rec.Code, c.want)
			}
		})
	}
}

func TestHandleWebhookTrigger_ErrorMappings(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{workflow.ErrWebhookNotFound, http.StatusNotFound},
		{workflow.ErrWebhookMethodNotAllowed, http.StatusMethodNotAllowed},
		{workflow.ErrWebhookUnauthorized, http.StatusUnauthorized},
		{workflow.ErrWebhookEntryRequired, http.StatusBadRequest},
		{workflow.ErrWebhookEntryNotFound, http.StatusNotFound},
		{workflow.ErrWebhookEntryForbidden, http.StatusForbidden},
		{workflow.ErrWebhookNoTriggerNode, http.StatusUnprocessableEntity},
		{workflow.ErrWorkflowNotActive, http.StatusConflict},
		{workflow.ErrWorkspaceAtCapacity, http.StatusTooManyRequests},
		{workflow.ErrWorkflowNotFound, http.StatusInternalServerError},
	}
	for _, c := range cases {
		h := &Handler{triggerUC: &stubTriggerUC{err: c.err}}
		rec := httptest.NewRecorder()
		h.HandleWebhookTrigger(rec, triggerReq("tok"))
		if rec.Code != c.want {
			t.Fatalf("err %v: got %d want %d", c.err, rec.Code, c.want)
		}
	}
}

func configReq(method, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "/workflows/wf1/webhook", nil)
	} else {
		r = httptest.NewRequest(method, "/workflows/wf1/webhook", strings.NewReader(body))
	}
	return mux.SetURLVars(r, map[string]string{"id": "wf1"})
}

func sampleWebhook() *workflow.WorkflowWebhook {
	return &workflow.WorkflowWebhook{
		ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "tok",
		AuthMode: workflow.WebhookAuthHeaderToken, Secret: "s", HeaderName: "X-Token", Method: "POST", Active: true,
	}
}

func TestWebhookConfigHandlers(t *testing.T) {
	t.Run("get found", func(t *testing.T) {
		h := &Handler{configUC: &stubConfigUC{wh: sampleWebhook()}}
		rec := httptest.NewRecorder()
		h.GetWorkflowWebhook(rec, configReq(http.MethodGet, ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "/webhooks/workflow/tok") {
			t.Fatalf("expected public url in body: %s", rec.Body.String())
		}
	})

	t.Run("get not configured", func(t *testing.T) {
		h := &Handler{configUC: &stubConfigUC{wh: nil}}
		rec := httptest.NewRecorder()
		h.GetWorkflowWebhook(rec, configReq(http.MethodGet, ""))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("got %d", rec.Code)
		}
	})

	t.Run("create", func(t *testing.T) {
		h := &Handler{configUC: &stubConfigUC{wh: sampleWebhook()}}
		rec := httptest.NewRecorder()
		h.CreateWorkflowWebhook(rec, configReq(http.MethodPost, `{"auth_mode":"header_token"}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("got %d", rec.Code)
		}
	})

	t.Run("create conflict", func(t *testing.T) {
		h := &Handler{configUC: &stubConfigUC{err: workflow.ErrWebhookAlreadyExists}}
		rec := httptest.NewRecorder()
		h.CreateWorkflowWebhook(rec, configReq(http.MethodPost, ""))
		if rec.Code != http.StatusConflict {
			t.Fatalf("got %d", rec.Code)
		}
	})

	t.Run("update", func(t *testing.T) {
		h := &Handler{configUC: &stubConfigUC{wh: sampleWebhook()}}
		rec := httptest.NewRecorder()
		h.UpdateWorkflowWebhook(rec, configReq(http.MethodPut, `{"active":false}`))
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d", rec.Code)
		}
	})

	t.Run("rotate", func(t *testing.T) {
		h := &Handler{configUC: &stubConfigUC{wh: sampleWebhook()}}
		rec := httptest.NewRecorder()
		h.RotateWorkflowWebhook(rec, configReq(http.MethodPost, ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d", rec.Code)
		}
	})

	t.Run("delete", func(t *testing.T) {
		stub := &stubConfigUC{}
		h := &Handler{configUC: stub}
		rec := httptest.NewRecorder()
		h.DeleteWorkflowWebhook(rec, configReq(http.MethodDelete, ""))
		if rec.Code != http.StatusNoContent || !stub.deleted {
			t.Fatalf("got %d deleted=%v", rec.Code, stub.deleted)
		}
	})

	t.Run("config error mappings", func(t *testing.T) {
		for _, tc := range []struct {
			err  error
			want int
		}{
			{workflow.ErrWorkflowNotFound, http.StatusNotFound},
			{workflow.ErrWebhookNotFound, http.StatusNotFound},
			{workflow.ErrWebhookInvalidAuthMode, http.StatusBadRequest},
			{workflow.ErrWorkspaceIDRequired, http.StatusInternalServerError},
		} {
			h := &Handler{configUC: &stubConfigUC{err: tc.err}}
			rec := httptest.NewRecorder()
			h.GetWorkflowWebhook(rec, configReq(http.MethodGet, ""))
			if rec.Code != tc.want {
				t.Fatalf("err %v: got %d want %d", tc.err, rec.Code, tc.want)
			}
		}
	})
}
