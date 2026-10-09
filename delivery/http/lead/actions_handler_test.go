package lead

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/advertising"
	"vozko/domain/cache"
	"vozko/domain/leadaction"
	"vozko/domain/report"
	"vozko/domain/selection"
	lead_usecase "vozko/usecases/lead"
	leadaction_usecase "vozko/usecases/leadaction"
)

const actionRunID = "0b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f"

type stubActions struct {
	started  []leadaction_usecase.Request
	previews []leadaction_usecase.Request
	outcome  leadaction_usecase.Outcome
	preview  *leadaction.Preview
	run      *leadaction.Run
	audience *leadaction.AudienceJob
	err      error
	asked    lead_usecase.Actor
}

func (s *stubActions) Audience(_ context.Context, a lead_usecase.Actor, _ string) (*leadaction.AudienceJob, error) {
	s.asked = a
	return s.audience, s.err
}

func (s *stubActions) Start(_ context.Context, req leadaction_usecase.Request) (leadaction_usecase.Outcome, error) {
	s.started = append(s.started, req)
	return s.outcome, s.err
}

func (s *stubActions) Preview(_ context.Context, req leadaction_usecase.Request) (*leadaction.Preview, error) {
	s.previews = append(s.previews, req)
	return s.preview, s.err
}

func (s *stubActions) PreviewStatus(_ context.Context, a lead_usecase.Actor, _ string) (*leadaction.Preview, error) {
	s.asked = a
	return s.preview, s.err
}

func (s *stubActions) Run(_ context.Context, a lead_usecase.Actor, _ string) (*leadaction.Run, error) {
	s.asked = a
	return s.run, s.err
}

func actionsHandler(actions Actions) http.Handler {
	h := NewLeadHandler(HandlerDeps{Commands: &stubCommands{}, History: &stubHistory{}, Actions: actions})
	router := mux.NewRouter()
	RegisterRoutes(router, h, passThrough)
	return router
}

func classifyBody() map[string]any {
	return map[string]any{
		"action": "classify",
		"params": map[string]any{"key": "interesse", "value": nil},
		"selection": map[string]any{
			"mode": "all_matching", "fingerprint": "fp", "expectedCount": 12,
			"filter": map[string]any{"groups": []any{map[string]any{"conjunction": "and", "predicates": []any{map[string]any{"field": "blocked", "operator": "is_false"}}}}},
			"sort":   []any{map[string]any{"field": "name", "desc": true}},
		},
	}
}

func TestStartActionNeedsAnIdempotencyKeyAndAnswersTheRun(t *testing.T) {
	actions := &stubActions{outcome: leadaction_usecase.Outcome{Run: &leadaction.Run{ID: actionRunID, Action: leadaction.ActionClassify, Status: leadaction.StatusQueued}}}
	if rec := send(t, actionsHandler(actions), http.MethodPost, "/leads/actions", nil, classifyBody()); rec.Code != http.StatusBadRequest || len(actions.started) != 0 {
		t.Fatalf("without the key = %d %s", rec.Code, rec.Body.String())
	}
	rec := send(t, actionsHandler(actions), http.MethodPost, "/leads/actions", map[string]string{"Idempotency-Key": "k-1", "X-Department-ID": ""}, classifyBody())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	var out ActionStartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Run == nil || out.Run.ID != actionRunID || out.Action != "classify" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	req := actions.started[0]
	if req.IdempotencyKey != "k-1" || req.Actor.UserID != "user-1" || req.Selection.Mode != selection.ModeAllMatching ||
		req.Selection.ExpectedCount != 12 || req.Selection.Fingerprint != "fp" || len(req.Selection.Sort) != 1 || !req.Selection.Sort[0].Desc {
		t.Fatalf("request = %+v", req)
	}
	if string(req.Params.Value) != "null" || req.Params.Key != "interesse" {
		t.Fatalf("an explicit null must reach the use case to clear the field: %+v", req.Params)
	}
}

func TestStartActionAnswersTheDownstreamOfOtherActions(t *testing.T) {
	export := &stubActions{outcome: leadaction_usecase.Outcome{Report: &report.Job{ID: "job-1", Kind: report.KindLeads}}}
	rec := send(t, actionsHandler(export), http.MethodPost, "/leads/actions", map[string]string{"Idempotency-Key": "k"}, map[string]any{
		"action": "export", "params": map[string]any{"addresses": true}, "selection": map[string]any{"mode": "ids", "ids": []string{actionRunID}},
	})
	var out ActionStartResponse
	if rec.Code != http.StatusAccepted || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Report == nil || out.Report.ID != "job-1" {
		t.Fatalf("export = %d %s", rec.Code, rec.Body.String())
	}
	audience := &stubActions{outcome: leadaction_usecase.Outcome{Audience: &leadaction.AudienceJob{ID: actionRunID, Status: leadaction.AudiencePending}}}
	rec = send(t, actionsHandler(audience), http.MethodPost, "/leads/actions", map[string]string{"Idempotency-Key": "k"}, map[string]any{
		"action": "meta_audience", "params": map[string]any{"adAccountId": "a", "name": "n"}, "selection": map[string]any{"mode": "ids", "ids": []string{actionRunID}},
	})
	out = ActionStartResponse{}
	if rec.Code != http.StatusAccepted || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Audience == nil || out.Audience.Status != "pending" || out.Audience.ID != actionRunID {
		t.Fatalf("audience = %d %s", rec.Code, rec.Body.String())
	}
}

func TestAnAudienceJobIsReadAsTheCaller(t *testing.T) {
	meta := advertising.Audience{MetaID: "aud-1", Name: "Base"}
	actions := &stubActions{audience: &leadaction.AudienceJob{ID: actionRunID, Status: leadaction.AudienceDone, Audience: &meta, Matched: 3}}
	rec := send(t, actionsHandler(actions), http.MethodGet, "/leads/actions/audiences/"+actionRunID, nil, nil)
	var out LeadAudienceResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Audience == nil || out.Audience.MetaID != "aud-1" || out.Matched != 3 || actions.asked.UserID != "user-1" {
		t.Fatalf("audience = %d %s", rec.Code, rec.Body.String())
	}
	if rec := send(t, actionsHandler(&stubActions{err: leadaction.ErrAudienceNotFound}), http.MethodGet, "/leads/actions/audiences/"+actionRunID, nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown audience = %d", rec.Code)
	}
}

func TestStartActionRefusesUnknownKeys(t *testing.T) {
	body := classifyBody()
	body["extra"] = true
	if rec := send(t, actionsHandler(&stubActions{}), http.MethodPost, "/leads/actions", map[string]string{"Idempotency-Key": "k"}, body); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown key = %d %s", rec.Code, rec.Body.String())
	}
}

func TestActionErrorsKeepTheirStatusAndCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{leadaction.ErrForbidden, http.StatusForbidden, "forbidden"},
		{leadaction.ErrIdempotencyKeyReused, http.StatusConflict, "idempotency_key_reused"},
		{leadaction.ErrInProgress, http.StatusConflict, "lead_action_in_progress"},
		{leadaction.ErrParamsAmbiguous, http.StatusBadRequest, "lead_action_params_ambiguous"},
		{leadaction.ErrSelectionTooLarge, http.StatusRequestEntityTooLarge, "lead_action_selection_too_large"},
		{selection.ErrFingerprintMismatch, http.StatusBadRequest, "selection_fingerprint_mismatch"},
		{selection.ErrScopeDenied, http.StatusForbidden, "selection_scope_denied"},
		{report.ErrNotAllowed, http.StatusForbidden, "report_forbidden"},
		{leadaction.ErrPhoneUnavailable, http.StatusUnprocessableEntity, "lead_action_phone_unavailable"},
		{selection.ErrCountRequired, http.StatusBadRequest, "selection_count_required"},
		{advertising.ErrSensitiveAudience, http.StatusUnprocessableEntity, "audience_sensitive_filter"},
		{advertising.ErrAudienceTermsNotAccepted, http.StatusConflict, "audience_terms_not_accepted"},
		{cache.ErrGateBusy, http.StatusServiceUnavailable, ""},
	}
	for _, tc := range cases {
		rec := send(t, actionsHandler(&stubActions{err: tc.err}), http.MethodPost, "/leads/actions", map[string]string{"Idempotency-Key": "k"}, classifyBody())
		if rec.Code != tc.status {
			t.Errorf("%v = %d %s", tc.err, rec.Code, rec.Body.String())
			continue
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Code != tc.code {
			t.Errorf("%v code = %q, want %q", tc.err, body.Code, tc.code)
		}
	}
	changed := &selection.CountChangedError{Expected: 12, Matched: 15}
	rec := send(t, actionsHandler(&stubActions{err: changed}), http.MethodPost, "/leads/actions", map[string]string{"Idempotency-Key": "k"}, classifyBody())
	var body struct {
		Code     string `json:"code"`
		Expected int    `json:"expected"`
		Matched  int    `json:"matched"`
	}
	if rec.Code != http.StatusConflict || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Matched != 15 || body.Code != "selection_changed" {
		t.Fatalf("a moved count = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPreviewAnswersNowOrLater(t *testing.T) {
	done := &stubActions{preview: &leadaction.Preview{ID: "p-1", Status: leadaction.PreviewDone, Result: leadaction.PreviewResult{Matched: 12, Eligible: 10}}}
	rec := send(t, actionsHandler(done), http.MethodPost, "/leads/actions/preview", nil, classifyBody())
	var out ActionPreviewResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Result.Eligible != 10 || out.ID != "p-1" {
		t.Fatalf("done preview = %d %s", rec.Code, rec.Body.String())
	}
	running := &stubActions{preview: &leadaction.Preview{ID: "p-2", Status: leadaction.PreviewRunning}}
	if rec := send(t, actionsHandler(running), http.MethodPost, "/leads/actions/preview", nil, classifyBody()); rec.Code != http.StatusAccepted {
		t.Fatalf("running preview = %d %s", rec.Code, rec.Body.String())
	}
	if rec := send(t, actionsHandler(&stubActions{err: leadaction.ErrPreviewNotFound}), http.MethodGet, "/leads/actions/previews/"+actionRunID, nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown preview = %d", rec.Code)
	}
}

func TestActionRunIsReadAsTheCaller(t *testing.T) {
	actions := &stubActions{run: &leadaction.Run{ID: actionRunID, Action: leadaction.ActionBlock, Status: leadaction.StatusRunning, Result: leadaction.Result{Processed: 500}}}
	rec := send(t, actionsHandler(actions), http.MethodGet, "/leads/actions/"+actionRunID, nil, nil)
	var out LeadActionRunResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Result.Processed != 500 || actions.asked.UserID != "user-1" {
		t.Fatalf("run = %d %s", rec.Code, rec.Body.String())
	}
	if rec := send(t, actionsHandler(&stubActions{err: leadaction.ErrRunNotFound}), http.MethodGet, "/leads/actions/"+actionRunID, nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown run = %d", rec.Code)
	}
	if rec := send(t, actionsHandler(nil), http.MethodGet, "/leads/actions/"+actionRunID, nil, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("without actions = %d", rec.Code)
	}
}
