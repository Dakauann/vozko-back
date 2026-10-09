package lead

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/campaign"
	"vozko/domain/leadaction"
	wd "vozko/domain/workspace/workspace_department"
	leadaction_usecase "vozko/usecases/leadaction"
	leadsend_usecase "vozko/usecases/leadsend"
)

type scopeCapturingActions struct {
	*stubActions
	contexts []context.Context
}

func (s *scopeCapturingActions) Start(ctx context.Context, req leadaction_usecase.Request) (leadaction_usecase.Outcome, error) {
	s.contexts = append(s.contexts, ctx)
	return s.stubActions.Start(ctx, req)
}

type stubSends struct {
	requests []leadsend_usecase.SendRequest
	review   *campaign.SendReview
	err      error
	called   string
}

func (s *stubSends) Review(_ context.Context, req leadsend_usecase.SendRequest) (*campaign.SendReview, error) {
	s.requests, s.called = append(s.requests, req), "review"
	return s.review, s.err
}

func (s *stubSends) Start(_ context.Context, req leadsend_usecase.SendRequest) (*campaign.SendReview, error) {
	s.requests, s.called = append(s.requests, req), "start"
	return s.review, s.err
}

func (s *stubSends) Cancel(_ context.Context, req leadsend_usecase.SendRequest) error {
	s.requests, s.called = append(s.requests, req), "cancel"
	return s.err
}

func sendsHandler(actions Actions, sends Sends) http.Handler {
	h := NewLeadHandler(HandlerDeps{Commands: &stubCommands{}, History: &stubHistory{}, Actions: actions, Sends: sends})
	router := mux.NewRouter()
	RegisterRoutes(router, h, passThrough)
	return router
}

func templateSendBody() map[string]any {
	return map[string]any{
		"action": "send_template",
		"params": map[string]any{"send": map[string]any{
			"name": "Matrículas", "businessPhoneId": "bp-1", "templateId": "tpl-1", "departmentId": "dept-1",
			"bindings": []any{map[string]any{"source": "lead.first_name"}, map[string]any{"source": "literal", "value": "2027"}},
		}},
		"selection": map[string]any{"mode": "ids", "ids": []any{"00000000-0000-4000-8000-000000000001"}},
	}
}

func TestASendActionCarriesItsParamsAndTheRequestersCreationScope(t *testing.T) {
	review := &campaign.SendReview{Channel: campaign.ChannelOfficial, Parts: []campaign.SendPart{{CampaignID: "c-1"}}, Entries: 1, Eligible: 1}
	actions := &scopeCapturingActions{stubActions: &stubActions{outcome: leadaction_usecase.Outcome{Send: review}}}
	rec := send(t, sendsHandler(actions, &stubSends{}), http.MethodPost, "/leads/actions", map[string]string{"Idempotency-Key": "k"}, templateSendBody())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	req := actions.started[0]
	if req.Action != leadaction.ActionSendTemplate || req.Params.Send == nil || req.Params.Send.TemplateID != "tpl-1" || len(req.Params.Send.Bindings) != 2 {
		t.Fatalf("request = %+v", req)
	}
	scope, ok := wd.GetCreationScope(actions.contexts[0])
	if !ok || scope.UserID != "user-1" {
		t.Fatalf("creation scope = %+v %v", scope, ok)
	}
	var body struct {
		Send *campaign.SendReview `json:"send"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Send == nil || body.Send.Parts[0].CampaignID != "c-1" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestSendRoutesPassTheChannelAndTheParts(t *testing.T) {
	sends := &stubSends{review: &campaign.SendReview{Channel: campaign.ChannelUnofficial}}
	handler := sendsHandler(&stubActions{}, sends)
	for path, called := range map[string]string{"/leads/actions/sends/review": "review", "/leads/actions/sends/start": "start", "/leads/actions/sends/cancel": "cancel"} {
		rec := send(t, handler, http.MethodPost, path, nil, map[string]any{"channel": "unofficial", "campaignIds": []string{"c-1", "c-2"}, "firstN": 40})
		if rec.Code != http.StatusOK || sends.called != called {
			t.Fatalf("%s = %d %s (called %s)", path, rec.Code, rec.Body.String(), sends.called)
		}
		last := sends.requests[len(sends.requests)-1]
		if last.Channel != campaign.ChannelUnofficial || len(last.CampaignIDs) != 2 || last.FirstN != 40 || last.Actor.UserID != "user-1" {
			t.Fatalf("%s request = %+v", path, last)
		}
	}
	if rec := send(t, handler, http.MethodPost, "/leads/actions/sends/start", nil, map[string]any{"channel": "official", "campaignIds": []string{"c-1"}, "extra": true}); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown field = %d", rec.Code)
	}
}

func TestSendRoutesRefuseWithoutTheSendService(t *testing.T) {
	rec := send(t, sendsHandler(&stubActions{}, nil), http.MethodPost, "/leads/actions/sends/review", nil, map[string]any{"channel": "official", "campaignIds": []string{"c-1"}})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestSendErrorsKeepTheirStatusAndCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
		fits   int
	}{
		{&campaign.BudgetRefusal{Reason: campaign.ErrUnaffordable, Fits: 1040}, http.StatusConflict, "unaffordable", 1040},
		{&campaign.BudgetRefusal{Reason: campaign.ErrOverMonthlyCap, Fits: 7}, http.StatusConflict, "over_cap", 7},
		{campaign.ErrCreationScopeMissing, http.StatusForbidden, "send_creation_scope_missing", 0},
		{campaign.ErrDepartmentRequired, http.StatusBadRequest, "send_department_required", 0},
		{campaign.ErrSelectionOverCampaignCap, http.StatusRequestEntityTooLarge, "send_selection_over_campaign_cap", 0},
		{campaign.ErrHeaderVariableUnsupported, http.StatusBadRequest, "send_header_variable_unsupported", 0},
		{campaign.ErrBindingSensitive, http.StatusBadRequest, "send_binding_sensitive", 0},
		{campaign.ErrAlreadyStarted, http.StatusConflict, "send_already_started", 0},
		{campaign.ErrSendIncomplete, http.StatusBadRequest, "send_incomplete", 0},
		{campaign.ErrNotFromSelection, http.StatusBadRequest, "send_not_from_selection", 0},
		{wd.ErrDepartmentAccessDenied, http.StatusForbidden, "send_department_forbidden", 0},
		{leadaction.ErrForbidden, http.StatusForbidden, "forbidden", 0},
	}
	for _, tc := range cases {
		rec := send(t, sendsHandler(&stubActions{}, &stubSends{err: tc.err}), http.MethodPost, "/leads/actions/sends/start", nil, map[string]any{"channel": "official", "campaignIds": []string{"c-1"}})
		var body struct {
			Code string `json:"code"`
			Fits int    `json:"fits"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != tc.status || body.Code != tc.code || body.Fits != tc.fits {
			t.Errorf("%v = %d %s, want %d %s fits %d", tc.err, rec.Code, rec.Body.String(), tc.status, tc.code, tc.fits)
		}
	}
}
