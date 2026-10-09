package calllisthttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/calls/callhistory"
	"vozko/domain/calls/calllist"
	"vozko/domain/shared"
	"vozko/domain/sip_trunk"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	calllist_usecase "vozko/usecases/calls/calllist"
)

var at = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

type serviceSpy struct {
	err     error
	actor   calllist_usecase.Actor
	query   calllist.ListQuery
	items   calllist.ItemQuery
	change  calllist.Change
	closing calllist.Closing
	id      string
	deleted bool
	next    *calllist_usecase.NextResult
	nextAt  *time.Time
	asOf    *time.Time
}

func theList() *calllist.List {
	return &calllist.List{ID: someList, WorkspaceID: "ws-1", Name: "Retorno", CreatedBy: "manager-1", AssigneeIDs: []string{"worker-1"},
		Status: calllist.StatusActive, Phone: calllist.PhoneChoice{Source: calllist.PhoneIdentity},
		Selected: 10, ItemCount: 8, ClosedCount: 3, CalledCount: 5, CallbackCount: 2, Skipped: calllist.Skips{calllist.SkipNoNumber: 2}, CreatedAt: at, UpdatedAt: at}
}

func theItem() *calllist.Item {
	until := at.Add(calllist.ReservationTTL)
	return &calllist.Item{ID: someItem, ListID: someList, WorkspaceID: "ws-1", LeadID: "lead-1", Phone: "5511987654321", Position: 4,
		State: calllist.StateReserved, ReservedBy: "worker-1", ReservedUntil: &until, CreatedAt: at, UpdatedAt: at}
}

func theView() calllist_usecase.ListView {
	return calllist_usecase.ListView{List: theList(), Verdict: calllist.ListVerdict{AcceptsOutcomes: true,
		StatusMoves: []calllist.Status{calllist.StatusPaused, calllist.StatusArchived}}}
}

func (s *serviceSpy) Lists(_ context.Context, a calllist_usecase.Actor, q calllist.ListQuery) (calllist_usecase.ListViews, error) {
	s.actor, s.query = a, q
	return calllist_usecase.ListViews{Lists: []calllist_usecase.ListView{theView()}, Total: 1}, s.err
}

func (s *serviceSpy) View(_ context.Context, a calllist_usecase.Actor, id string) (calllist_usecase.ListView, error) {
	s.actor, s.id = a, id
	return theView(), s.err
}

func (s *serviceSpy) Update(_ context.Context, a calllist_usecase.Actor, id string, c calllist.Change) (calllist_usecase.ListView, error) {
	s.actor, s.id, s.change = a, id, c
	return theView(), s.err
}

func (s *serviceSpy) Delete(_ context.Context, a calllist_usecase.Actor, id string) error {
	s.actor, s.id, s.deleted = a, id, true
	return s.err
}

func (s *serviceSpy) Items(_ context.Context, a calllist_usecase.Actor, q calllist.ItemQuery) (calllist_usecase.ItemRows, error) {
	s.actor, s.items = a, q
	row := calllist_usecase.ItemRow{ItemView: calllist.ItemView{Item: *theItem(), LeadName: "Maria", LeadDistrict: "Aldeia", LeadCity: "Barueri", Attempts: 2},
		Outcome: callhistory.OutcomeNoAnswer, Closable: true}
	return calllist_usecase.ItemRows{Items: []calllist_usecase.ItemRow{row}, Next: 4, NextAt: s.nextAt, AsOf: s.asOf}, s.err
}

func (s *serviceSpy) Next(_ context.Context, a calllist_usecase.Actor, listID string) (*calllist_usecase.NextResult, error) {
	s.actor, s.id = a, listID
	return s.next, s.err
}

func (s *serviceSpy) Release(_ context.Context, a calllist_usecase.Actor, itemID string) (calllist_usecase.ItemVerdict, error) {
	s.actor, s.id = a, itemID
	return calllist_usecase.ItemVerdict{Item: theItem(), Closable: true}, s.err
}

func (s *serviceSpy) Close(_ context.Context, a calllist_usecase.Actor, itemID string, c calllist.Closing) (calllist_usecase.ItemVerdict, error) {
	s.actor, s.id, s.closing = a, itemID, c
	item := theItem()
	item.State, item.Disposition = calllist.StateClosed, c.Disposition
	return calllist_usecase.ItemVerdict{Item: item}, s.err
}

func serve(t *testing.T, spy *serviceSpy, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	allow := func(_ workspace_domain.Resource, _ workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return next
	}
	var h *Handler
	if spy != nil {
		h = NewHandler(spy)
	} else {
		h = NewHandler(nil)
	}
	RegisterProtectedRoutes(router, h, allow)
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := context.WithValue(request.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "worker-1", Role: "user"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request.WithContext(ctx))
	return recorder
}

func decode(t *testing.T, r *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("body %s: %v", r.Body, err)
	}
	return envelope
}

func data(t *testing.T, r *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	return decode(t, r)
}

func TestListingCallListsAnswersAPageWithProgress(t *testing.T) {
	spy := &serviceSpy{}
	got := serve(t, spy, http.MethodGet, "/call-lists?status=active&page=2&pageSize=10", "")
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", got.Code, got.Body)
	}
	if spy.actor.UserID != "worker-1" || spy.actor.WorkspaceID != "ws-1" || spy.query.Status != calllist.StatusActive || spy.query.Page != 2 || spy.query.PageSize != 10 {
		t.Fatalf("asked %+v %+v", spy.actor, spy.query)
	}
	body := data(t, got)
	lists := body["items"].([]any)
	first := lists[0].(map[string]any)
	if first["id"] != someList || first["itemCount"] != float64(8) || first["closedCount"] != float64(3) || first["openCount"] != float64(5) ||
		body["total"] != float64(1) {
		t.Fatalf("body = %v", body)
	}
	if skipped := first["skipped"].(map[string]any); skipped["no_number"] != float64(2) {
		t.Fatalf("skipped = %v", skipped)
	}
}

func TestAnUnknownStatusFilterIsRefused(t *testing.T) {
	got := serve(t, &serviceSpy{}, http.MethodGet, "/call-lists?status=deleted", "")
	if got.Code != http.StatusBadRequest || decode(t, got)["code"] != "call_list_status_invalid" {
		t.Fatalf("status = %d: %s", got.Code, got.Body)
	}
}

func TestPatchingAListSendsOnlyTheFieldsGiven(t *testing.T) {
	spy := &serviceSpy{}
	got := serve(t, spy, http.MethodPatch, "/call-lists/"+someList, `{"status":"paused","assigneeIds":["worker-2"]}`)
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", got.Code, got.Body)
	}
	if spy.change.Name != nil || spy.change.Status == nil || *spy.change.Status != calllist.StatusPaused || (*spy.change.AssigneeIDs)[0] != "worker-2" {
		t.Fatalf("change = %+v", spy.change)
	}
	if got := serve(t, spy, http.MethodPatch, "/call-lists/"+someList, `{"color":"red"}`); got.Code != http.StatusBadRequest {
		t.Fatalf("an unknown field = %d", got.Code)
	}
}

func TestDeletingAListAnswersNoContent(t *testing.T) {
	spy := &serviceSpy{}
	got := serve(t, spy, http.MethodDelete, "/call-lists/"+someList, "")
	if got.Code != http.StatusNoContent || !spy.deleted || spy.id != someList {
		t.Fatalf("status = %d deleted %v", got.Code, spy.deleted)
	}
}

func TestItemsArePagedByPositionWithTheirTechnicalOutcome(t *testing.T) {
	spy := &serviceSpy{}
	got := serve(t, spy, http.MethodGet, "/call-lists/"+someList+"/items?state=closed&after=12&limit=30", "")
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", got.Code, got.Body)
	}
	if spy.items.ListID != someList || spy.items.State != calllist.StateClosed || spy.items.AfterPosition != 12 || spy.items.Limit != 30 {
		t.Fatalf("query = %+v", spy.items)
	}
	body := data(t, got)
	item := body["items"].([]any)[0].(map[string]any)
	if item["leadName"] != "Maria" || item["outcome"] != "no_answer" || item["attempts"] != float64(2) || body["next"] != float64(4) {
		t.Fatalf("body = %v", body)
	}
	if got := serve(t, spy, http.MethodGet, "/call-lists/"+someList+"/items?after=x", ""); got.Code != http.StatusBadRequest {
		t.Fatalf("a cursor that is not a position = %d", got.Code)
	}
}

func TestNextAnswersTheItemTheLeadCardAndTheLines(t *testing.T) {
	spy := &serviceSpy{next: &calllist_usecase.NextResult{
		List: theList(), Item: theItem(), Lead: calllist.LeadCard{ID: "lead-1", Name: "Maria", District: "Centro", FamilyCount: 3},
		LastInteraction: &calllist.LastInteraction{EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp, At: at},
		Trunks:          []sip_trunk.TrunkChoice{{ID: "trunk-1", Name: "Matriz"}}, Refused: 1,
	}}
	got := serve(t, spy, http.MethodPost, "/call-lists/"+someList+"/next", "")
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", got.Code, got.Body)
	}
	body := data(t, got)
	lead := body["lead"].(map[string]any)
	if body["item"].(map[string]any)["id"] != someItem || lead["district"] != "Centro" || lead["familyCount"] != float64(3) ||
		body["lastInteraction"].(map[string]any)["entryId"] != "entry-1" || len(body["trunks"].([]any)) != 1 || body["refused"] != float64(1) {
		t.Fatalf("body = %v", body)
	}

	empty := &serviceSpy{next: &calllist_usecase.NextResult{List: theList()}}
	done := data(t, serve(t, empty, http.MethodPost, "/call-lists/"+someList+"/next", ""))
	if done["item"] != nil || done["lead"] != nil {
		t.Fatalf("an empty queue answered %v", done)
	}
}

func TestClosingSendsTheOutcomeNoteAndCallbackTime(t *testing.T) {
	spy := &serviceSpy{}
	got := serve(t, spy, http.MethodPost, "/call-list-items/"+someItem+"/close", `{"disposition":"_callback","note":"depois","callbackAt":"2026-10-09T15:00:00Z"}`)
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", got.Code, got.Body)
	}
	if spy.closing.Disposition != "_callback" || spy.closing.Note != "depois" || spy.closing.CallbackAt == nil || spy.id != someItem {
		t.Fatalf("closing = %+v", spy.closing)
	}
	if got := serve(t, spy, http.MethodPost, "/call-list-items/"+someItem+"/close", `{"disposition":"x","callId":"c"}`); got.Code != http.StatusBadRequest {
		t.Fatalf("a call id from the client must be refused, got %d", got.Code)
	}
}

func TestEachRefusalHasItsStatusAndCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{calllist.ErrForbidden, http.StatusForbidden, "forbidden"},
		{calllist.ErrNotAssignee, http.StatusForbidden, "call_list_not_assignee"},
		{calllist.ErrListNotFound, http.StatusNotFound, "call_list_not_found"},
		{calllist.ErrItemNotFound, http.StatusNotFound, "call_list_item_not_found"},
		{calllist.ErrCallNotTheItems, http.StatusConflict, "call_list_call_not_the_items"},
		{calllist.ErrItemTaken, http.StatusConflict, "call_list_item_taken"},
		{calllist.ErrReservationHeld, http.StatusConflict, "call_list_reservation_held"},
		{calllist.ErrListBuilding, http.StatusConflict, "call_list_building"},
		{calllist.ErrNoOutcomes, http.StatusUnprocessableEntity, "call_list_no_outcomes"},
		{calllist.ErrDispositionUnknown, http.StatusBadRequest, "call_list_disposition_unknown"},
		{errors.New("boom"), http.StatusInternalServerError, ""},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			got := serve(t, &serviceSpy{err: tc.err}, http.MethodPost, "/call-list-items/"+someItem+"/release", "")
			if got.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", got.Code, tc.status, got.Body)
			}
			if tc.code != "" && decode(t, got)["code"] != tc.code {
				t.Fatalf("code = %v, want %s", decode(t, got)["code"], tc.code)
			}
		})
	}
}

func TestAServerWithoutCallListsAnswersUnavailable(t *testing.T) {
	got := serve(t, nil, http.MethodGet, "/call-lists", "")
	if got.Code != http.StatusServiceUnavailable || decode(t, got)["code"] != "call_lists_unavailable" {
		t.Fatalf("status = %d: %s", got.Code, got.Body)
	}
}

func TestAListTakesNoSignOff(t *testing.T) {
	spy := &serviceSpy{}
	if got := serve(t, spy, http.MethodPatch, "/call-lists/"+someList, `{"signOff":true}`); got.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", got.Code, got.Body)
	}
	if spy.actor.UserID != "" {
		t.Fatalf("a refused body reached the service: %+v", spy.change)
	}
	body := data(t, serve(t, spy, http.MethodGet, "/call-lists/"+someList, ""))
	for _, gone := range []string{"needsSignOff", "signedOffBy", "signedOffAt"} {
		if _, ok := body[gone]; ok {
			t.Fatalf("the list still answers %s: %v", gone, body)
		}
	}
}

func TestNextSaysWhenTheQueueMayHoldMore(t *testing.T) {
	spy := &serviceSpy{next: &calllist_usecase.NextResult{List: theList(), Refused: 25, More: true}}
	body := data(t, serve(t, spy, http.MethodPost, "/call-lists/"+someList+"/next", ""))
	if body["more"] != true || body["item"] != nil || body["refused"] != float64(25) {
		t.Fatalf("body = %v", body)
	}
}

func TestAnItemRowHidesALeadNameThatIsOnlyItsNumber(t *testing.T) {
	row := calllist_usecase.ItemRow{ItemView: calllist.ItemView{Item: *theItem(), LeadName: "5511987654321", LeadNumber: "5511987654321"}}
	if got := rowResponseOf(row); got.LeadName != "" {
		t.Fatalf("leadName = %q, want the placeholder hidden", got.LeadName)
	}
}

func TestAListCarriesItsProgressAndTheVerdictsTheScreenRenders(t *testing.T) {
	spy := &serviceSpy{}
	for _, target := range []string{"/call-lists", "/call-lists/" + someList} {
		body := data(t, serve(t, spy, http.MethodGet, target, ""))
		list := body
		if items, ok := body["items"].([]any); ok {
			list = items[0].(map[string]any)
		}
		if list["calledCount"] != float64(5) || list["callbackCount"] != float64(2) || list["acceptsOutcomes"] != true {
			t.Fatalf("%s answered %v", target, list)
		}
		moves := list["statusMoves"].([]any)
		if len(moves) != 2 || moves[0] != "paused" || moves[1] != "archived" {
			t.Fatalf("%s moves = %v", target, moves)
		}
	}
}

func TestAListAnswersTheVerdictTheServiceDecided(t *testing.T) {
	view := theView()
	view.Verdict = calllist.ListVerdict{StatusMoves: []calllist.Status{}}
	got := ViewResponseOf(view)
	if got.AcceptsOutcomes || got.StatusMoves == nil || len(got.StatusMoves) != 0 {
		t.Fatalf("response = %+v, want the verdict as given", got)
	}
	if got := ViewResponseOf(theView()); !got.AcceptsOutcomes || len(got.StatusMoves) != 2 {
		t.Fatalf("response = %+v", got)
	}
}

func TestTheProgressCountersNeverGoBelowZero(t *testing.T) {
	view := theView()
	view.CalledCount, view.CallbackCount, view.ClosedCount, view.ItemCount = -1, -2, 9, 8
	got := ViewResponseOf(view)
	if got.CalledCount != 0 || got.CallbackCount != 0 || got.OpenCount != 0 {
		t.Fatalf("called %d, callbacks %d, open %d, want 0", got.CalledCount, got.CallbackCount, got.OpenCount)
	}
}

func TestThePendingTabCarriesTheCursorOfItsAgenda(t *testing.T) {
	asOf := at
	nextAt := at.Add(-time.Hour)
	spy := &serviceSpy{asOf: &asOf, nextAt: &nextAt}
	target := "/call-lists/" + someList + "/items?state=pending&after=9&afterAt=2026-10-08T11:00:00.123456Z&asOf=2026-10-08T12:00:00Z"
	got := serve(t, spy, http.MethodGet, target, "")
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", got.Code, got.Body)
	}
	wantAfter := time.Date(2026, 10, 8, 11, 0, 0, 123456000, time.UTC)
	if spy.items.AfterPosition != 9 || spy.items.AfterAt == nil || !spy.items.AfterAt.Equal(wantAfter) || !spy.items.AsOf.Equal(at) {
		t.Fatalf("query = %+v", spy.items)
	}
	body := data(t, got)
	if body["next"] != float64(4) || body["nextAt"] != "2026-10-08T11:00:00Z" || body["asOf"] != "2026-10-08T12:00:00Z" {
		t.Fatalf("body = %v", body)
	}

	queued := &serviceSpy{asOf: &asOf}
	plain := data(t, serve(t, queued, http.MethodGet, "/call-lists/"+someList+"/items?state=pending&after=9&asOf=2026-10-08T12:00:00Z", ""))
	if _, has := plain["nextAt"]; has || queued.items.AfterAt != nil {
		t.Fatalf("a queued cursor carries no time: body %v, query %+v", plain, queued.items)
	}

	for _, bad := range []string{"afterAt=yesterday", "asOf=2026-10-08", "asOf=12:00"} {
		if got := serve(t, &serviceSpy{}, http.MethodGet, "/call-lists/"+someList+"/items?state=pending&after=9&"+bad, ""); got.Code != http.StatusBadRequest {
			t.Fatalf("%s answered %d", bad, got.Code)
		}
	}

	refused := serve(t, &serviceSpy{err: calllist.ErrItemCursorInvalid}, http.MethodGet, "/call-lists/"+someList+"/items?state=pending&after=9", "")
	if refused.Code != http.StatusBadRequest || !strings.Contains(refused.Body.String(), "call_list_items_cursor_invalid") {
		t.Fatalf("a later page without asOf = %d %s", refused.Code, refused.Body)
	}
}

func TestEachItemSaysWhetherTheViewerCanCloseItAndWhereTheLeadLives(t *testing.T) {
	body := data(t, serve(t, &serviceSpy{}, http.MethodGet, "/call-lists/"+someList+"/items?state=pending", ""))
	item := body["items"].([]any)[0].(map[string]any)
	if item["closable"] != true || item["leadDistrict"] != "Aldeia" || item["leadCity"] != "Barueri" {
		t.Fatalf("item = %v", item)
	}
	released := data(t, serve(t, &serviceSpy{}, http.MethodPost, "/call-list-items/"+someItem+"/release", ""))
	if released["closable"] != true {
		t.Fatalf("released = %v", released)
	}
	closed := data(t, serve(t, &serviceSpy{}, http.MethodPost, "/call-list-items/"+someItem+"/close", `{"disposition":"interessado"}`))
	if closed["closable"] != false {
		t.Fatalf("closed = %v", closed)
	}
}

func TestNextSaysWhetherTheHeldContactCanBeClosedAndCarriesTheListVerdict(t *testing.T) {
	spy := &serviceSpy{next: &calllist_usecase.NextResult{List: theList(), Item: theItem(), Lead: calllist.LeadCard{ID: "lead-1", District: "Aldeia", City: "Barueri"},
		Closable: true, Verdict: calllist.ListVerdict{AcceptsOutcomes: true, StatusMoves: []calllist.Status{calllist.StatusPaused}}}}
	body := data(t, serve(t, spy, http.MethodPost, "/call-lists/"+someList+"/next", ""))
	item := body["item"].(map[string]any)
	list := body["list"].(map[string]any)
	if item["closable"] != true || item["leadDistrict"] != "Aldeia" || item["leadCity"] != "Barueri" || list["acceptsOutcomes"] != true {
		t.Fatalf("body = %v", body)
	}
}
