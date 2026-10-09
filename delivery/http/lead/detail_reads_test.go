package lead

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/conversation"
	leaddomain "vozko/domain/lead"
	workspace_domain "vozko/domain/workspace"
	lead_usecase "vozko/usecases/lead"
)

type stubSummaries struct {
	summary lead_usecase.LeadDetailSummary
	err     error
	asked   []string
}

func (s *stubSummaries) Summary(_ context.Context, v conversation.Viewer, leadID string) (lead_usecase.LeadDetailSummary, error) {
	s.asked = append(s.asked, v.WorkspaceID+"/"+leadID)
	return s.summary, s.err
}

func summaryRouter(summaries Summaries) http.Handler {
	h := NewLeadHandler(HandlerDeps{Commands: &stubCommands{}, History: &stubHistory{}, Summaries: summaries})
	router := mux.NewRouter()
	RegisterRoutes(router, h, passThrough)
	return router
}

func TestTheDetailAnswersTheOwnerNameAndTheConsentPurposeButNoSectionData(t *testing.T) {
	granted := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	history := &stubHistory{detail: &lead_usecase.LeadDetail{
		Lead: &leaddomain.Lead{ID: routeLeadID, Version: 2, Number: "5511987654321", Owner: "u-1",
			WhatsAppOptIn: &leaddomain.Consent{GrantedAt: granted, Source: leaddomain.ConsentManual, Purpose: "Avisos da matrícula"}},
		OwnerName: "Marina Costa",
	}}
	rec := send(t, routedHandler(&stubCommands{}, history), http.MethodGet, "/leads/"+routeLeadID, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var out LeadDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.OwnerName != "Marina Costa" {
		t.Fatalf("owner name = %q", out.OwnerName)
	}
	if out.WhatsAppOptIn == nil || out.WhatsAppOptIn.Purpose != "Avisos da matrícula" || out.WhatsAppOptIn.Source != "manual" {
		t.Fatalf("consent = %+v", out.WhatsAppOptIn)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"dealsCount", "memoriesCount", "sharedNumbers"} {
		if _, present := raw[key]; present {
			t.Fatalf("%s belongs to GET /leads/{id}/summary, not to the detail", key)
		}
	}
}

func TestTheSummaryAnswersTheTabCountsAndSharedNumbers(t *testing.T) {
	deals := 3
	summaries := &stubSummaries{summary: lead_usecase.LeadDetailSummary{
		DealsCount: &deals, MemoriesCount: 5,
		SharedNumbers: []leaddomain.SharedNumber{{Number: "551133334444", More: true,
			Holders: []leaddomain.NumberHolder{{LeadID: relativeLeadID, Name: "Joana Lima", Number: "5511912345678"}}}},
	}}
	rec := send(t, summaryRouter(summaries), http.MethodGet, "/leads/"+routeLeadID+"/summary", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var out LeadDetailSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.DealsCount == nil || *out.DealsCount != 3 || out.MemoriesCount != 5 {
		t.Fatalf("summary = deals %v memories %d", out.DealsCount, out.MemoriesCount)
	}
	if len(out.SharedNumbers) != 1 || out.SharedNumbers[0].Number != "551133334444" || !out.SharedNumbers[0].More {
		t.Fatalf("shared numbers = %+v", out.SharedNumbers)
	}
	if holder := out.SharedNumbers[0].Holders[0]; holder.LeadID != relativeLeadID || holder.Name != "Joana Lima" || holder.Number != "5511912345678" {
		t.Fatalf("holder = %+v", holder)
	}
	if len(summaries.asked) != 1 || summaries.asked[0] != "ws-1/"+routeLeadID {
		t.Fatalf("asked = %v", summaries.asked)
	}
}

func TestTheSummaryOfALeadWithoutDealAccessOrSharedNumbers(t *testing.T) {
	rec := send(t, summaryRouter(&stubSummaries{}), http.MethodGet, "/leads/"+routeLeadID+"/summary", nil, nil)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if _, present := out["dealsCount"]; present {
		t.Fatal("a viewer without deals must get no deal count, not zero")
	}
	if shared, ok := out["sharedNumbers"].([]any); !ok || len(shared) != 0 {
		t.Fatalf("sharedNumbers = %v, want an empty list", out["sharedNumbers"])
	}
	if memories, ok := out["memoriesCount"].(float64); !ok || memories != 0 {
		t.Fatalf("memoriesCount = %v, want 0", out["memoriesCount"])
	}
}

func TestTheSummaryAnswersItsOwnErrors(t *testing.T) {
	cases := []struct {
		name      string
		summaries Summaries
		status    int
		code      string
	}{
		{"no leads:read", &stubSummaries{err: leaddomain.ErrLeadForbidden}, http.StatusForbidden, "forbidden"},
		{"a lead that does not exist", &stubSummaries{err: leaddomain.ErrLeadNotFound}, http.StatusNotFound, "lead_not_found"},
		{"a failed read", &stubSummaries{err: errors.New("db down")}, http.StatusInternalServerError, ""},
		{"not wired", nil, http.StatusServiceUnavailable, "lead_history_unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := send(t, summaryRouter(tc.summaries), http.MethodGet, "/leads/"+routeLeadID+"/summary", nil, nil)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.code == "" {
				return
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["code"] != tc.code {
				t.Fatalf("body = %s, want code %s", rec.Body.String(), tc.code)
			}
		})
	}
}

func TestTheSummaryRouteNeedsLeadsRead(t *testing.T) {
	gates := map[string]gatedRoute{}
	h := NewLeadHandler(HandlerDeps{Commands: &stubCommands{}, History: &stubHistory{}, Summaries: &stubSummaries{}})
	router := mux.NewRouter()
	RegisterRoutes(router, h, func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			path, _ := mux.CurrentRoute(r).GetPathTemplate()
			gates[r.Method+" "+path] = gatedRoute{resource, action}
		}
	})
	send(t, router, http.MethodGet, "/leads/"+routeLeadID+"/summary", nil, nil)
	if got := gates["GET /leads"+leadIDPath+"/summary"]; got != (gatedRoute{workspace_domain.ResourceLeads, workspace_domain.ActionRead}) {
		t.Fatalf("the summary is gated by %+v", got)
	}
}

func TestTheInboxCardAnswersTheOwnerNameAndTheOptOut(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	history := &stubHistory{card: leaddomain.Card{LeadID: routeLeadID, Version: 4, Owner: "u-1", OwnerName: "Marina Costa",
		OptedOutAt: &at, OptOutSource: leaddomain.OptOutLeadRequest}}
	rec := send(t, routedHandler(&stubCommands{}, history), http.MethodGet, "/entries/"+relativeLeadID+"/lead", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var out LeadCardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.OwnerName != "Marina Costa" || out.OptedOutAt == nil || *out.OptedOutAt != "2026-10-08T12:00:00Z" || out.OptOutSource != "lead_request" {
		t.Fatalf("card = %+v", out)
	}

	plain := send(t, routedHandler(&stubCommands{}, &stubHistory{card: leaddomain.Card{LeadID: routeLeadID}}), http.MethodGet, "/entries/"+relativeLeadID+"/lead", nil, nil)
	var raw map[string]any
	if err := json.Unmarshal(plain.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"optedOutAt", "optOutSource", "ownerName"} {
		if _, present := raw[key]; present {
			t.Fatalf("%s must be absent on a lead that never opted out and has no owner", key)
		}
	}
}
