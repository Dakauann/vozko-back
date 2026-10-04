package whatsappbusinessphone

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	wc "vozko/domain/whatsapp_campaign"
	"vozko/infra/http/middleware"
)

type receptiveStub struct {
	settings wc.ReceptiveSettings
	saved    *wc.ReceptiveSettings
	ws       string
	err      error
}

func (s *receptiveStub) Get(workspaceID, _ string) (wc.ReceptiveSettings, error) {
	s.ws = workspaceID
	return s.settings, s.err
}

func (s *receptiveStub) Update(workspaceID, _ string, settings wc.ReceptiveSettings) (wc.ReceptiveSettings, error) {
	s.ws, s.saved = workspaceID, &settings
	return settings, s.err
}

func receptiveRequest(method, body string) *http.Request {
	r := httptest.NewRequest(method, "/whatsapp/business-phones/phone-1/automation", strings.NewReader(body))
	r = mux.SetURLVars(r, map[string]string{"id": "phone-1"})
	return r.WithContext(context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, "owner"))
}

func receptiveHandler(uc ReceptiveUseCase) *WhatsAppBusinessPhoneHandler {
	h := &WhatsAppBusinessPhoneHandler{}
	h.SetReceptive(uc)
	return h
}

func decodeAutomation(t *testing.T, rec *httptest.ResponseRecorder) NumberAutomationResponse {
	t.Helper()
	var got NumberAutomationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, rec.Body.String())
	}
	return got
}

func TestTheNumbersAutomationIsReadInTheChannelShape(t *testing.T) {
	uc := &receptiveStub{settings: wc.ReceptiveSettings{AgentID: "agent-1", EnableAgentResponses: true, EnableAnalysis: true}}
	rec := httptest.NewRecorder()
	receptiveHandler(uc).GetNumberAutomation(rec, receptiveRequest(http.MethodGet, ""))
	if rec.Code != http.StatusOK || uc.ws != "owner" {
		t.Fatalf("status %d ws %q", rec.Code, uc.ws)
	}
	got := decodeAutomation(t, rec)
	if got.ID != "phone-1" || got.AgentID == nil || *got.AgentID != "agent-1" || got.WorkflowID != nil || !got.EnableAgentResponses || !got.EnableAnalysis {
		t.Fatalf("got %+v", got)
	}
}

func TestTheNumbersAutomationIsSaved(t *testing.T) {
	uc := &receptiveStub{}
	rec := httptest.NewRecorder()
	body := `{"agentId":null,"workflowId":"flow-1","pipelineId":"pipe-1","enableWorkflow":true,"enableAutoStaging":true}`
	receptiveHandler(uc).UpdateNumberAutomation(rec, receptiveRequest(http.MethodPut, body))
	if rec.Code != http.StatusOK || uc.saved == nil {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	want := wc.ReceptiveSettings{WorkflowID: "flow-1", PipelineID: "pipe-1", EnableWorkflow: true, EnableAutoStaging: true}
	if *uc.saved != want {
		t.Fatalf("saved %+v", *uc.saved)
	}
}

func TestNumberAutomationErrorsMapToStatuses(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{wc.ErrReceptiveNotOwner, http.StatusForbidden},
		{wc.ErrCampaignBusinessPhoneNotFound, http.StatusNotFound},
		{errors.New("database down"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		receptiveHandler(&receptiveStub{err: tc.err}).GetNumberAutomation(rec, receptiveRequest(http.MethodGet, ""))
		if rec.Code != tc.want {
			t.Errorf("%v: status %d", tc.err, rec.Code)
		}
	}
}

func TestNumberAutomationRefusesWhenUnavailable(t *testing.T) {
	rec := httptest.NewRecorder()
	(&WhatsAppBusinessPhoneHandler{}).UpdateNumberAutomation(rec, receptiveRequest(http.MethodPut, `{"enableAnalysis":true}`))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestNumberAutomationRejectsAMalformedBody(t *testing.T) {
	uc := &receptiveStub{}
	rec := httptest.NewRecorder()
	receptiveHandler(uc).UpdateNumberAutomation(rec, receptiveRequest(http.MethodPut, `{`))
	if rec.Code != http.StatusBadRequest || uc.saved != nil {
		t.Fatalf("status %d", rec.Code)
	}
}
