package lead

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/mux"

	leaddomain "vozko/domain/lead"
	workspace_domain "vozko/domain/workspace"
	lead_usecase "vozko/usecases/lead"
)

func (s *stubCommands) RecordConsent(_ context.Context, _ lead_usecase.Actor, _ string, in lead_usecase.ConsentInput) (lead_usecase.ConsentResult, error) {
	s.consent = in
	l, err := s.answer("consent", nil)
	if err != nil {
		return lead_usecase.ConsentResult{}, err
	}
	return lead_usecase.ConsentResult{Lead: l, Changed: true, EventID: "ev-1"}, nil
}

func TestTheConsentRouteIsGatedByLeadsUpdate(t *testing.T) {
	gates := map[string]gatedRoute{}
	h := NewLeadHandler(HandlerDeps{Commands: &stubCommands{result: &leaddomain.Lead{ID: routeLeadID}}, History: &stubHistory{}})
	router := mux.NewRouter()
	RegisterRoutes(router, h, func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			path, _ := mux.CurrentRoute(r).GetPathTemplate()
			gates[r.Method+" "+path] = gatedRoute{resource, action}
		}
	})
	send(t, router, http.MethodPost, "/leads/"+routeLeadID+"/consent", nil, nil)
	if got := gates["POST /leads"+leadIDPath+"/consent"]; got != (gatedRoute{workspace_domain.ResourceLeads, workspace_domain.ActionUpdate}) {
		t.Fatalf("POST /leads/{id}/consent gate = %+v, want leads:update (grants also need leads:consent in the use case)", got)
	}
}

func TestTheConsentRoutePassesTheChangeAndAnswersTheOutcome(t *testing.T) {
	granted := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 4,
		WhatsAppOptIn: &leaddomain.Consent{GrantedAt: granted, Source: leaddomain.ConsentManual, Purpose: "novidades"}}}
	body := map[string]any{"kind": "grant", "source": "manual", "purpose": "novidades",
		"evidence": map[string]any{"type": "note", "excerpt": "confirmou por telefone em 09/10"}}

	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/consent", nil, body)

	if rec.Code != http.StatusOK || len(cmds.calls) != 1 || cmds.calls[0] != "consent" {
		t.Fatalf("status = %d calls = %v body %s", rec.Code, cmds.calls, rec.Body.String())
	}
	want := lead_usecase.ConsentInput{Kind: leaddomain.ConsentGrant, Source: leaddomain.ConsentManual, Purpose: "novidades",
		Evidence: leaddomain.ConsentEvidence{Type: leaddomain.EvidenceNote, Excerpt: "confirmou por telefone em 09/10"}}
	if cmds.consent != want {
		t.Fatalf("input = %+v, want %+v", cmds.consent, want)
	}
	var out RecordConsentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || !out.Changed || out.EventID != "ev-1" || out.Lead.WhatsAppOptIn == nil || out.Lead.ID != routeLeadID {
		t.Fatalf("answer = %s", rec.Body.String())
	}
}

func TestTheConsentRouteCarriesWhoAskedOnAWithdrawal(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID}}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/consent", nil,
		map[string]any{"kind": "withdraw", "source": "manual", "requestedBy": "lead_request"})
	if rec.Code != http.StatusOK || cmds.consent.RequestedBy != leaddomain.OptOutLeadRequest || cmds.consent.Kind != leaddomain.ConsentWithdraw {
		t.Fatalf("status = %d input = %+v", rec.Code, cmds.consent)
	}
}

func TestTheConsentRouteCodesItsRefusals(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{leaddomain.ErrConsentEvidenceRequired, http.StatusBadRequest, "lead_consent_evidence_required"},
		{leaddomain.ErrConsentSourceUnknown, http.StatusBadRequest, "lead_consent_source_unknown"},
		{leaddomain.ErrConsentKindInvalid, http.StatusBadRequest, "lead_consent_kind_invalid"},
		{leaddomain.ErrConsentExcerptTooLong, http.StatusBadRequest, "lead_consent_excerpt_too_long"},
		{leaddomain.ErrConsentSourceForbidden, http.StatusForbidden, "lead_consent_source_forbidden"},
		{leaddomain.ErrLeadForbidden, http.StatusForbidden, "forbidden"},
		{leaddomain.ErrLeadNotFound, http.StatusNotFound, "lead_not_found"},
	}
	for _, tc := range cases {
		rec := send(t, routedHandler(&stubCommands{err: tc.err}, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/consent", nil,
			map[string]any{"kind": "grant", "source": "manual"})
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != tc.status || body.Code != tc.code {
			t.Errorf("%v: status %d code %q, want %d %q", tc.err, rec.Code, body.Code, tc.status, tc.code)
		}
	}
	rec := send(t, routedHandler(&stubCommands{}, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/consent", nil, map[string]any{"kind": "grant", "why": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown key: status = %d, want 400", rec.Code)
	}
}
