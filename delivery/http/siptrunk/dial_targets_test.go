package siptrunk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/lead"
	"vozko/domain/sip_trunk"
	"vozko/infra/http/middleware"
	lead_usecase "vozko/usecases/lead"
)

type plannerSpy struct {
	plan   *lead_usecase.LeadDialPlan
	err    error
	actor  lead_usecase.Actor
	leadID string
	lined  bool
}

func (p *plannerSpy) Lines(_ context.Context, actor lead_usecase.Actor) (*lead_usecase.LeadDialPlan, error) {
	p.actor, p.lined = actor, true
	return p.plan, p.err
}

func (p *plannerSpy) Plan(_ context.Context, actor lead_usecase.Actor, leadID string) (*lead_usecase.LeadDialPlan, error) {
	p.actor, p.leadID = actor, leadID
	return p.plan, p.err
}

func askDialTargets(t *testing.T, h *Handler, target string, role string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	ctx := context.WithValue(request.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "ana", Role: role})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	recorder := httptest.NewRecorder()
	h.DialTargets(recorder, request.WithContext(ctx))
	return recorder
}

func TestDialTargetsAnswerTheLeadNumbersAndLines(t *testing.T) {
	spy := &plannerSpy{plan: &lead_usecase.LeadDialPlan{
		LeadID: "lead-1",
		DialPlan: lead.DialPlan{
			Numbers: []lead.PlannedDialNumber{
				{DialNumber: lead.DialNumber{Number: "5584994409684", Identity: true}},
				{DialNumber: lead.DialNumber{Number: "551133334444", Label: lead.PhoneLandline, PhoneID: "phone-1"}, Refusal: lead.DialRefusedInvalidNumber},
			},
			Callable: "5584994409684",
		},
		Trunks: []sip_trunk.TrunkChoice{{ID: "trunk-1", Name: "Matriz"}},
	}}
	got := askDialTargets(t, NewHandler(HandlerDeps{}).WithDialTargets(spy), "/dial-targets?leadId=lead-1", "user")
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", got.Code, got.Body)
	}
	if spy.leadID != "lead-1" || spy.actor != (lead_usecase.Actor{UserID: "ana", WorkspaceID: "ws-1"}) {
		t.Fatalf("asked %q as %+v", spy.leadID, spy.actor)
	}
	var body map[string]any
	if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := `{"callable":"5584994409684","leadId":"lead-1","numbers":[{"identity":true,"number":"5584994409684"},{"identity":false,"label":"landline","number":"551133334444","phoneId":"phone-1","refusal":"invalid_number"}],"trunks":[{"id":"trunk-1","name":"Matriz"}]}`
	if encoded, _ := json.Marshal(body); string(encoded) != want {
		t.Fatalf("body = %s\nwant %s", encoded, want)
	}
}

func TestDialTargetsCarryTheReasonsALeadCannotBeCalled(t *testing.T) {
	spy := &plannerSpy{plan: &lead_usecase.LeadDialPlan{LeadID: "lead-1", DialPlan: lead.DialPlan{Refusal: lead.DialRefusedBlocked, Numbers: []lead.PlannedDialNumber{}}, TrunkRefusal: lead_usecase.TrunkRefusedNoneDialable}}
	got := askDialTargets(t, NewHandler(HandlerDeps{}).WithDialTargets(spy), "/dial-targets?leadId=lead-1", "admin")
	var body DialTargetsResponse
	if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Refusal != "blocked" || body.TrunkRefusal != "no_dialable_trunk" || body.Numbers == nil || body.Trunks == nil {
		t.Fatalf("body = %+v", body)
	}
	if !spy.actor.IsAdmin {
		t.Fatal("a platform admin must be planned as one")
	}
}

func TestDialTargetsRefusals(t *testing.T) {
	cases := []struct {
		name   string
		target string
		spy    *plannerSpy
		status int
		code   string
	}{
		{name: "may not read leads", target: "/dial-targets?leadId=lead-1", spy: &plannerSpy{err: lead.ErrLeadForbidden}, status: http.StatusForbidden, code: "forbidden"},
		{name: "unknown lead", target: "/dial-targets?leadId=lead-1", spy: &plannerSpy{err: lead.ErrLeadNotFound}, status: http.StatusNotFound, code: "lead_not_found"},
		{name: "failure", target: "/dial-targets?leadId=lead-1", spy: &plannerSpy{err: errors.New("database down")}, status: http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := askDialTargets(t, NewHandler(HandlerDeps{}).WithDialTargets(tc.spy), tc.target, "user")
			if got.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", got.Code, tc.status, got.Body)
			}
			if tc.code != "" {
				var body struct {
					Code string `json:"code"`
				}
				_ = json.Unmarshal(got.Body.Bytes(), &body)
				if body.Code != tc.code {
					t.Fatalf("code = %q, want %q", body.Code, tc.code)
				}
			}
		})
	}
}

func TestDialTargetsAreUnavailableWithoutAPlanner(t *testing.T) {
	got := askDialTargets(t, NewHandler(HandlerDeps{}), "/dial-targets?leadId=lead-1", "user")
	if got.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when nothing can plan the call", got.Code)
	}
}

func TestDialTargetsNeedASignedInCaller(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/dial-targets?leadId=lead-1", nil)
	ctx := context.WithValue(request.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	recorder := httptest.NewRecorder()
	spy := &plannerSpy{plan: &lead_usecase.LeadDialPlan{LeadID: "lead-1"}}
	NewHandler(HandlerDeps{}).WithDialTargets(spy).DialTargets(recorder, request.WithContext(ctx))
	if recorder.Code != http.StatusUnauthorized || spy.leadID != "" {
		t.Fatalf("status = %d, planned %q; a caller without claims is never planned", recorder.Code, spy.leadID)
	}
}

func TestDialTargetsWithoutALeadOfferOnlyTheLines(t *testing.T) {
	for _, target := range []string{"/dial-targets", "/dial-targets?leadId=%20"} {
		spy := &plannerSpy{plan: &lead_usecase.LeadDialPlan{Trunks: []sip_trunk.TrunkChoice{{ID: "trunk-1", Name: "Matriz"}}}}
		got := askDialTargets(t, NewHandler(HandlerDeps{}).WithDialTargets(spy), target, "user")
		if got.Code != http.StatusOK || !spy.lined || spy.leadID != "" {
			t.Fatalf("%s: status = %d, lines asked %v, lead %q: %s", target, got.Code, spy.lined, spy.leadID, got.Body)
		}
		if spy.actor != (lead_usecase.Actor{UserID: "ana", WorkspaceID: "ws-1"}) {
			t.Fatalf("lines asked as %+v", spy.actor)
		}
		var body map[string]any
		if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		want := `{"leadId":"","numbers":[],"trunks":[{"id":"trunk-1","name":"Matriz"}]}`
		if encoded, _ := json.Marshal(body); string(encoded) != want {
			t.Fatalf("body = %s, want %s", encoded, want)
		}
	}
}

func TestDialTargetsWithoutALeadFailWhenTheLinesCannotBeRead(t *testing.T) {
	got := askDialTargets(t, NewHandler(HandlerDeps{}).WithDialTargets(&plannerSpy{err: errors.New("trunks down")}), "/dial-targets", "user")
	if got.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d: %s", got.Code, got.Body)
	}
}
