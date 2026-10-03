package copilottools

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/calls/callhistory"
	"vozko/domain/calls/cdr"
	"vozko/domain/copilot"
	"vozko/domain/shared"
	"vozko/domain/workspace"
	callhistory_usecase "vozko/usecases/callhistory"
)

type historyStub struct {
	listed callhistory_usecase.ListInput
	viewer callhistory_usecase.Viewer
	detail *callhistory.Detail
}

func (h *historyStub) List(_ context.Context, input callhistory_usecase.ListInput) (*shared.PaginatedResult[callhistory.Summary], error) {
	h.listed = input
	answeredAt := time.Date(2026, 9, 30, 14, 0, 12, 0, time.UTC)
	return &shared.PaginatedResult[callhistory.Summary]{Page: 1, PageSize: 20, TotalItems: 1, TotalPages: 1, Items: []callhistory.Summary{{
		CallID: "call-1", Direction: cdr.DirectionInbound, Channel: callhistory.ChannelPhone, Outcome: callhistory.OutcomeAnswered,
		StartedAt: time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC), AnsweredAt: &answeredAt, TalkSeconds: 185, RingSeconds: 12,
		Contact:    callhistory.Contact{Number: "+5584999990000", LeadID: "lead-1", Name: "Maria"},
		AnsweredBy: &callhistory.Person{ID: "u-ana", Name: "Ana"}, Transfers: 1,
		Charge: &callhistory.Charge{Micros: 1_250_000, Settled: true},
	}}}, nil
}

func (h *historyStub) Get(_ context.Context, viewer callhistory_usecase.Viewer, callID string) (*callhistory.Detail, error) {
	h.viewer = viewer
	if h.detail == nil || h.detail.CallID != callID {
		return nil, cdr.ErrCallNotFound
	}
	return h.detail, nil
}

type permissionSet map[workspace.PermissionEntry]bool

func (p permissionSet) Execute(_, _ string, resource workspace.Resource, action workspace.Action) error {
	if p[workspace.PermissionEntry{Resource: resource, Action: action}] {
		return nil
	}
	return errors.New("denied")
}

func TestAnOperatorAsksAboutCallsAndSeesOnlyTheirOwn(t *testing.T) {
	history := &historyStub{}
	tool := NewListCallsTool(CallHistoryDeps{History: history, Permissions: permissionSet{}})
	res := tool.Execute(context.Background(), accessCtx, map[string]interface{}{
		"direction": "inbound", "result": "unanswered", "date_from": "2026-09-01", "date_to": "2026-09-30", "page_size": 500,
	})
	if res.Status != copilot.StatusOK {
		t.Fatalf("res = %+v", res)
	}
	in := history.listed
	if in.Viewer.SeesEveryone || in.Viewer.UserID != "user-1" || in.Viewer.WorkspaceID != "ws-1" {
		t.Fatalf("viewer = %+v", in.Viewer)
	}
	if *in.Direction != cdr.DirectionInbound || *in.Answered || in.PageSize != maxCallPageSize {
		t.Fatalf("input = %+v", in)
	}
	if in.To.Sub(*in.From) < 29*24*time.Hour {
		t.Fatalf("the period must cover both days: from=%v to=%v", in.From, in.To)
	}
	data := res.Data.(map[string]interface{})
	if data["scope"] != "só as ligações de que o usuário participou" {
		t.Fatalf("scope = %v", data["scope"])
	}
	call := data["calls"].([]map[string]interface{})[0]
	if call["contact"] != "Maria" || call["answered_by"] != "Ana" || call["charged_brl"] != 1.25 || call["placed_by"] != nil {
		t.Fatalf("call = %+v", call)
	}
}

func TestAManagerSeesTheWholeTeamsCalls(t *testing.T) {
	history := &historyStub{}
	grants := permissionSet{{Resource: workspace.ResourceCallHistory, Action: workspace.ActionViewOthers}: true}
	NewListCallsTool(CallHistoryDeps{History: history, Permissions: grants}).Execute(context.Background(), accessCtx, nil)
	if !history.listed.Viewer.SeesEveryone {
		t.Fatal("a manager with call_history:view_others must see the team")
	}
}

func TestAnUnreadableDateIsRefused(t *testing.T) {
	res := NewListCallsTool(CallHistoryDeps{History: &historyStub{}, Permissions: permissionSet{}}).Execute(context.Background(), accessCtx, map[string]interface{}{"date_from": "ontem"})
	if res.Status != copilot.StatusError {
		t.Fatalf("res = %+v", res)
	}
}

func TestACallTellsItsStoryByName(t *testing.T) {
	history := &historyStub{detail: &callhistory.Detail{
		Summary:  callhistory.Summary{CallID: "call-1", Outcome: callhistory.OutcomeAnswered, Contact: callhistory.Contact{Number: "+5584999990000"}},
		Handlers: []callhistory.Person{{ID: "u-ana", Name: "Ana"}, {ID: "u-bruno", Name: "Bruno"}},
		Timeline: []callhistory.NamedEntry{
			{TimelineEntry: callhistory.TimelineEntry{Kind: callhistory.EntryTransferRequested, Notes: "cliente quer falar de boleto"},
				Actor: &callhistory.Person{Name: "Ana"}, QueueName: "Financeiro"},
			{TimelineEntry: callhistory.TimelineEntry{Kind: callhistory.EntryTransferConnected}, Target: &callhistory.Person{Name: "Bruno"}},
		},
		Recording: &callhistory.Recording{URL: "https://files/rec.ogg"},
	}}
	res := NewGetCallTool(CallHistoryDeps{History: history, Permissions: permissionSet{}}).Execute(context.Background(), accessCtx, map[string]interface{}{"call_id": "call-1"})
	if res.Status != copilot.StatusOK {
		t.Fatalf("res = %+v", res)
	}
	data := res.Data.(map[string]interface{})
	timeline := data["timeline"].([]map[string]interface{})
	if timeline[0]["by"] != "Ana" || timeline[0]["queue"] != "Financeiro" || timeline[1]["to"] != "Bruno" {
		t.Fatalf("timeline = %+v", timeline)
	}
	if data["has_recording"] != true || data["contact"] != "+5584999990000" {
		t.Fatalf("data = %+v", data)
	}
	if _, leaked := data["recording_url"]; leaked {
		t.Fatal("the recording link must stay on the call history screen")
	}
}

func TestACallYouCannotSeeIsNotFound(t *testing.T) {
	res := NewGetCallTool(CallHistoryDeps{History: &historyStub{}, Permissions: permissionSet{}}).Execute(context.Background(), accessCtx, map[string]interface{}{"call_id": "someone-elses"})
	if res.Status != copilot.StatusError {
		t.Fatalf("res = %+v", res)
	}
}
