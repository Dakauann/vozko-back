package lead

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/mux"

	leaddomain "vozko/domain/lead"
	"vozko/domain/opportunity"
	"vozko/domain/shared"
	workspace_domain "vozko/domain/workspace"
	lead_usecase "vozko/usecases/lead"
)

type stubTimeline struct {
	page   leaddomain.TimelinePage
	deals  lead_usecase.DealsPage
	err    error
	asked  []leaddomain.PageQuery
	actors []lead_usecase.Actor
}

func (s *stubTimeline) Page(_ context.Context, a lead_usecase.Actor, q leaddomain.PageQuery) (leaddomain.TimelinePage, error) {
	s.asked, s.actors = append(s.asked, q), append(s.actors, a)
	return s.page, s.err
}

func (s *stubTimeline) Deals(_ context.Context, a lead_usecase.Actor, q leaddomain.PageQuery) (lead_usecase.DealsPage, error) {
	s.asked, s.actors = append(s.asked, q), append(s.actors, a)
	return s.deals, s.err
}

func timelineRouter(timeline Timeline) http.Handler {
	h := NewLeadHandler(HandlerDeps{Commands: &stubCommands{}, History: &stubHistory{}, Timeline: timeline})
	router := mux.NewRouter()
	RegisterRoutes(router, h, passThrough)
	RegisterEntryConversationRoutes(router, h, passThrough)
	return router
}

func TestTheTimelineAndDealsRoutesNeedLeadsRead(t *testing.T) {
	gates := map[string]gatedRoute{}
	h := NewLeadHandler(HandlerDeps{Commands: &stubCommands{}, History: &stubHistory{}, Timeline: &stubTimeline{}})
	router := mux.NewRouter()
	RegisterRoutes(router, h, func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			path, _ := mux.CurrentRoute(r).GetPathTemplate()
			gates[r.Method+" "+path] = gatedRoute{resource, action}
		}
	})
	send(t, router, http.MethodGet, "/leads/"+routeLeadID+"/timeline", nil, nil)
	send(t, router, http.MethodGet, "/leads/"+routeLeadID+"/deals", nil, nil)
	read := gatedRoute{workspace_domain.ResourceLeads, workspace_domain.ActionRead}
	for _, route := range []string{"GET /leads" + leadIDPath + "/timeline", "GET /leads" + leadIDPath + "/deals"} {
		if gates[route] != read {
			t.Errorf("%s is gated by %+v", route, gates[route])
		}
	}
}

func TestTheLeadConversationsRouteIsGoneInFavourOfTheTimeline(t *testing.T) {
	rec := send(t, timelineRouter(&stubTimeline{}), http.MethodGet, "/leads/"+routeLeadID+"/conversations", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, the route must be gone", rec.Code)
	}
}

func TestTheTimelineAnswersOnePageOfItems(t *testing.T) {
	at := time.Date(2026, 10, 8, 14, 3, 0, 0, time.UTC)
	answered := at.Add(4 * time.Second)
	timeline := &stubTimeline{page: leaddomain.TimelinePage{
		Items: []leaddomain.TimelineItem{
			{ID: "call:c-1", Kind: leaddomain.TimelineCall, At: at, Actor: "u-1", ActorName: "Sara", Ref: leaddomain.TimelineRef{Type: leaddomain.TimelineRefCall, ID: "provider-1"},
				Summary: leaddomain.TimelineSummary{Direction: "outbound", Status: "completed", DurationSec: 42, AnsweredAt: &answered, Source: "sip"}},
			{ID: "conversation:whatsapp:e-1", Kind: leaddomain.TimelineConversation, At: at.Add(-time.Hour),
				Ref: leaddomain.TimelineRef{Type: leaddomain.TimelineRefEntry, ID: "e-1", EntryType: shared.EntryTypeWhatsApp}, Summary: leaddomain.TimelineSummary{Channel: shared.EntryTypeWhatsApp}},
			{ID: "record:ev-1", Kind: leaddomain.TimelineRecord, At: at.Add(-2 * time.Hour), Actor: "system", Ref: leaddomain.TimelineRef{Type: leaddomain.TimelineRefEvent, ID: "ev-1"},
				Summary: leaddomain.TimelineSummary{Event: "updated", Fields: []string{"name"}}},
		},
		Next: "cursor-2",
	}}
	rec := send(t, timelineRouter(timeline), http.MethodGet, "/leads/"+routeLeadID+"/timeline?before=cursor-1&limit=20", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if len(timeline.asked) != 1 || timeline.asked[0] != (leaddomain.PageQuery{LeadID: routeLeadID, Before: "cursor-1", Limit: 20}) {
		t.Fatalf("asked %+v", timeline.asked)
	}
	if a := timeline.actors[0]; a.UserID != "user-1" || a.WorkspaceID != "ws-1" || a.IsAdmin {
		t.Fatalf("actor = %+v", a)
	}
	var out LeadTimelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.LeadID != routeLeadID || out.Next != "cursor-2" || len(out.Items) != 3 {
		t.Fatalf("response = %+v", out)
	}
	call := out.Items[0]
	if call.ID != "call:c-1" || call.Kind != "call" || call.At != "2026-10-08T14:03:00Z" || call.Actor != "u-1" || call.ActorName != "Sara" ||
		call.Ref != (TimelineRefResponse{Type: "call", ID: "provider-1"}) || call.Summary.Direction != "outbound" || call.Summary.DurationSec != 42 ||
		call.Summary.AnsweredAt != "2026-10-08T14:03:04Z" || call.Summary.Source != "sip" {
		t.Fatalf("call item = %+v", call)
	}
	if conv := out.Items[1]; conv.Ref != (TimelineRefResponse{Type: "entry", ID: "e-1", EntryType: "whatsapp"}) || conv.Summary.Channel != "whatsapp" {
		t.Fatalf("conversation item = %+v", conv)
	}
	if record := out.Items[2]; record.Summary.Event != "updated" || len(record.Summary.Fields) != 1 || record.Summary.Fields[0] != "name" {
		t.Fatalf("record item = %+v", record)
	}
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if items, _ := raw["items"].([]any); len(items) == 3 {
		if summary, _ := items[1].(map[string]any)["summary"].(map[string]any); len(summary) != 1 {
			t.Fatalf("empty summary fields must be left out: %v", summary)
		}
	}
}

func TestTheTimelineAnswersStageMovesAndCallOutcomes(t *testing.T) {
	at := time.Date(2026, 10, 8, 14, 3, 0, 0, time.UTC)
	callback := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	timeline := &stubTimeline{page: leaddomain.TimelinePage{Items: []leaddomain.TimelineItem{
		{ID: "deal_event:ev-1", Kind: leaddomain.TimelineDealEvent, At: at, Actor: "u-1", Ref: leaddomain.TimelineRef{Type: leaddomain.TimelineRefDeal, ID: "d-1"},
			Summary: leaddomain.TimelineSummary{Title: "Matrícula", Event: "stage_moved", PipelineID: "p-1", StageID: "s-2", FromStageID: "s-1",
				StageName: "Visita agendada", FromStageName: "Novo contato", ValueCents: 150000, Currency: "BRL"}},
		{ID: "call:c-1", Kind: leaddomain.TimelineCall, At: at.Add(-time.Hour), Ref: leaddomain.TimelineRef{Type: leaddomain.TimelineRefCall, ID: "provider-1"},
			Summary: leaddomain.TimelineSummary{Direction: "outbound", Disposition: "_callback", CallbackAt: &callback, CallListID: "list-1"}},
	}}}
	rec := send(t, timelineRouter(timeline), http.MethodGet, "/leads/"+routeLeadID+"/timeline", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var out LeadTimelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("items = %+v", out.Items)
	}
	move := out.Items[0]
	if move.Kind != "deal_event" || move.Ref != (TimelineRefResponse{Type: "deal", ID: "d-1"}) || move.Summary.Event != "stage_moved" ||
		move.Summary.StageID != "s-2" || move.Summary.FromStageID != "s-1" || move.Summary.StageName != "Visita agendada" ||
		move.Summary.FromStageName != "Novo contato" || move.Summary.Title != "Matrícula" {
		t.Fatalf("stage move = %+v", move)
	}
	if call := out.Items[1].Summary; call.Disposition != "_callback" || call.CallbackAt != "2026-10-09T09:00:00Z" || call.CallListID != "list-1" {
		t.Fatalf("call outcome = %+v", call)
	}
}

func TestAnEmptyTimelineAnswersAnEmptyList(t *testing.T) {
	rec := send(t, timelineRouter(&stubTimeline{}), http.MethodGet, "/leads/"+routeLeadID+"/timeline", nil, nil)
	if rec.Code != http.StatusOK || rec.Body.String() == "" {
		t.Fatalf("status = %d", rec.Code)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if items, ok := raw["items"].([]any); !ok || len(items) != 0 {
		t.Fatalf("items = %v, want []", raw["items"])
	}
	if _, has := raw["next"]; has {
		t.Fatalf("the last page has no next: %v", raw)
	}
}

func TestTheTimelineRoutesAnswerEachRefusalWithItsCode(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		err    error
		status int
		code   string
	}{
		{"a limit that is not a number", "/timeline?limit=many", nil, http.StatusBadRequest, "lead_page_invalid"},
		{"a cursor that was not ours", "/timeline", leaddomain.ErrPageQueryInvalid, http.StatusBadRequest, "lead_page_invalid"},
		{"no leads:read", "/timeline", leaddomain.ErrLeadForbidden, http.StatusForbidden, "forbidden"},
		{"a lead that does not exist", "/timeline", leaddomain.ErrLeadNotFound, http.StatusNotFound, "lead_not_found"},
		{"a failure", "/timeline", errors.New("db down"), http.StatusInternalServerError, ""},
		{"deals without deal access", "/deals", opportunity.ErrScopeDenied, http.StatusForbidden, "deals_forbidden"},
		{"deals of a lead that does not exist", "/deals", leaddomain.ErrLeadNotFound, http.StatusNotFound, "lead_not_found"},
		{"deals with a broken limit", "/deals?limit=-", nil, http.StatusBadRequest, "lead_page_invalid"},
		{"deals with a timeline cursor", "/deals", fmt.Errorf("deals: %w", leaddomain.ErrPageQueryInvalid), http.StatusBadRequest, "lead_page_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := send(t, timelineRouter(&stubTimeline{err: tc.err}), http.MethodGet, "/leads/"+routeLeadID+tc.path, nil, nil)
			if rec.Code != tc.status {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
			}
			if tc.code != "" {
				var body struct {
					Code string `json:"code"`
				}
				_ = json.Unmarshal(rec.Body.Bytes(), &body)
				if body.Code != tc.code {
					t.Fatalf("code = %q, want %q (%s)", body.Code, tc.code, rec.Body.String())
				}
			}
		})
	}
}

func TestTheTimelineRoutesRefuseWhenNotWired(t *testing.T) {
	for _, path := range []string{"/timeline", "/deals"} {
		rec := send(t, timelineRouter(nil), http.MethodGet, "/leads/"+routeLeadID+path, nil, nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
	}
}

func TestTheDealsOfALeadArePaged(t *testing.T) {
	created := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	timeline := &stubTimeline{deals: lead_usecase.DealsPage{
		Deals: []*opportunity.Opportunity{{ID: "d-1", LeadID: routeLeadID, Title: "Matrícula", Status: opportunity.StatusOpen, OwnerID: "u-1", OwnerName: "Sara", CreatedAt: created}},
		Next:  "cursor-2",
	}}
	rec := send(t, timelineRouter(timeline), http.MethodGet, "/leads/"+routeLeadID+"/deals?limit=5&before=cursor-1", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if timeline.asked[0] != (leaddomain.PageQuery{LeadID: routeLeadID, Before: "cursor-1", Limit: 5}) {
		t.Fatalf("asked %+v", timeline.asked[0])
	}
	var out LeadDealsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.LeadID != routeLeadID || out.Next != "cursor-2" || len(out.Deals) != 1 || out.Deals[0].ID != "d-1" || out.Deals[0].OwnerName != "Sara" {
		t.Fatalf("response = %+v", out)
	}
	empty := send(t, timelineRouter(&stubTimeline{}), http.MethodGet, "/leads/"+routeLeadID+"/deals", nil, nil)
	var raw map[string]any
	if err := json.Unmarshal(empty.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if deals, ok := raw["deals"].([]any); !ok || len(deals) != 0 {
		t.Fatalf("deals = %v, want []", raw["deals"])
	}
}
