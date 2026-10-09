package callhistory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/calls/callhistory"
	"vozko/domain/calls/cdr"
	"vozko/domain/shared"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	callhistory_usecase "vozko/usecases/callhistory"
)

var started = time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)

type historySpy struct {
	listed callhistory_usecase.ListInput
	viewer callhistory_usecase.Viewer
	err    error
}

func (h *historySpy) List(_ context.Context, input callhistory_usecase.ListInput) (*shared.PaginatedResult[callhistory.Summary], error) {
	h.listed = input
	if h.err != nil {
		return nil, h.err
	}
	answered := started.Add(8 * time.Second)
	return &shared.PaginatedResult[callhistory.Summary]{
		Items: []callhistory.Summary{{
			CallID: "sip-out-1", Direction: cdr.DirectionOutbound, Channel: callhistory.ChannelPhone, Outcome: callhistory.OutcomeAnswered,
			StartedAt: started, AnsweredAt: &answered, TalkSeconds: 120, RingSeconds: 8,
			Contact:  callhistory.Contact{Number: "5584994409684", LeadID: "lead-1", Name: "Maria"},
			PlacedBy: &callhistory.Person{ID: "ana", Name: "Ana"}, Transfers: 1,
			Charge: &callhistory.Charge{Micros: 26_666, Settled: true},
		}},
		Page: 2, PageSize: 25, TotalItems: 26, TotalPages: 2,
	}, nil
}

func (h *historySpy) Get(_ context.Context, viewer callhistory_usecase.Viewer, callID string) (*callhistory.Detail, error) {
	h.viewer = viewer
	if h.err != nil {
		return nil, h.err
	}
	return &callhistory.Detail{
		Summary:  callhistory.Summary{CallID: callID, Outcome: callhistory.OutcomeAnswered, StartedAt: started},
		Handlers: []callhistory.Person{{ID: "ana", Name: "Ana"}},
		Timeline: []callhistory.NamedEntry{{
			TimelineEntry: callhistory.TimelineEntry{Kind: callhistory.EntryTransferConnected, At: started, TargetQueueID: "q1"},
			Actor:         &callhistory.Person{ID: "bia", Name: "Bia"}, QueueName: "Suporte",
		}},
		Recording: &callhistory.Recording{URL: "https://files/rec.wav", DurationSec: 120},
	}, nil
}

type grants map[string]bool

func (g grants) Execute(_, _ string, resource workspace_domain.Resource, action workspace_domain.Action) error {
	if g[string(resource)+":"+string(action)] {
		return nil
	}
	return errors.New("forbidden")
}

func serve(t *testing.T, history History, access grants, target string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	gate := func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !access[string(resource)+":"+string(action)] {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next(w, r)
		}
	}
	RegisterProtectedRoutes(router, NewHandler(history, access), gate)
	request := httptest.NewRequest(http.MethodGet, target, nil)
	ctx := context.WithValue(request.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "ana"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws1")
	request = request.WithContext(ctx)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

var operator = grants{"call_history:read": true}

func TestTheHistoryIsClosedToPeopleWithoutAccess(t *testing.T) {
	for _, target := range []string{"/calls", "/calls/sip-out-1"} {
		if got := serve(t, &historySpy{}, grants{}, target); got.Code != http.StatusForbidden {
			t.Fatalf("%s = %d, want 403", target, got.Code)
		}
	}
}

func TestAnOperatorListsAsThemselvesWithoutTeamOrRecordingAccess(t *testing.T) {
	spy := &historySpy{}
	serve(t, spy, operator, "/calls")
	viewer := spy.listed.Viewer
	if viewer != (callhistory_usecase.Viewer{WorkspaceID: "ws1", UserID: "ana"}) {
		t.Fatalf("viewer = %+v", viewer)
	}
}

func TestPermissionsWidenWhatTheViewerSees(t *testing.T) {
	spy := &historySpy{}
	serve(t, spy, grants{"call_history:read": true, "call_history:view_others": true, "call_recordings:read": true}, "/calls/sip-out-1")
	if !spy.viewer.SeesEveryone || !spy.viewer.HearsRecordings {
		t.Fatalf("viewer = %+v", spy.viewer)
	}
}

func TestFiltersAreReadFromTheQuery(t *testing.T) {
	spy := &historySpy{}
	got := serve(t, spy, operator, "/calls?page=2&pageSize=25&direction=inbound&channel=whatsapp&result=unanswered&memberId=bia&from=2026-09-01&to=2026-09-30&number=8499")
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", got.Code, got.Body)
	}
	in := spy.listed
	if in.Page != 2 || in.PageSize != 25 || *in.Direction != cdr.DirectionInbound || *in.Channel != callhistory.ChannelWhatsApp || *in.Answered || in.MemberID != "bia" || in.Number != "8499" {
		t.Fatalf("input = %+v", in)
	}
	if in.From == nil || in.To == nil || in.To.Hour() != 23 {
		t.Fatalf("period = %v to %v, want whole days", in.From, in.To)
	}
}

func TestTheListNarrowsToTheCallsOfOneLead(t *testing.T) {
	spy := &historySpy{}
	got := serve(t, spy, operator, "/calls?leadId=6f1c2f9e-5b1d-4c84-9d0a-2f3b8c7e1a01")
	if got.Code != http.StatusOK || spy.listed.LeadID != "6f1c2f9e-5b1d-4c84-9d0a-2f3b8c7e1a01" {
		t.Fatalf("status %d, input %+v", got.Code, spy.listed)
	}
}

func TestUnknownFilterValuesAreRejected(t *testing.T) {
	for _, query := range []string{"direction=sideways", "channel=fax", "result=maybe", "from=yesterday", "leadId=lead-1"} {
		if got := serve(t, &historySpy{}, operator, "/calls?"+query); got.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", query, got.Code)
		}
	}
}

func TestTheListSpeaksTheFrontEndsShape(t *testing.T) {
	got := serve(t, &historySpy{}, operator, "/calls")
	var page CallListResponse
	if err := json.Unmarshal(got.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.TotalItems != 26 || page.TotalPages != 2 || page.Page != 2 || len(page.Items) != 1 {
		t.Fatalf("page = %+v", page)
	}
	call := page.Items[0]
	if call.CallID != "sip-out-1" || call.Outcome != "answered" || call.Channel != "phone" || call.TalkSeconds != 120 ||
		call.Contact.Name != "Maria" || call.PlacedBy == nil || call.PlacedBy.Name != "Ana" || call.AnsweredBy != nil ||
		call.Charge == nil || call.Charge.AmountMicros != 26_666 || !call.Charge.Settled || call.AnsweredAt == nil {
		t.Fatalf("call = %+v", call)
	}
}

func TestTheDetailCarriesTheTimelineAndRecording(t *testing.T) {
	got := serve(t, &historySpy{}, operator, "/calls/sip-out-1")
	var detail CallDetailResponse
	if err := json.Unmarshal(got.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.CallID != "sip-out-1" || len(detail.Handlers) != 1 || detail.Recording == nil || detail.Recording.URL != "https://files/rec.wav" {
		t.Fatalf("detail = %+v", detail)
	}
	entry := detail.Timeline[0]
	if entry.Kind != "transfer_connected" || entry.QueueName != "Suporte" || entry.Actor == nil || entry.Actor.Name != "Bia" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestACallOutsideYourReachIsNotFound(t *testing.T) {
	if got := serve(t, &historySpy{err: cdr.ErrCallNotFound}, operator, "/calls/sip-out-9"); got.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", got.Code)
	}
}
