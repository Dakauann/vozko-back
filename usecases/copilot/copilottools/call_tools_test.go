package copilottools

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/sip_trunk"
	"vozko/domain/workspace"
)

const trunkUUID = "5f0c2b1e-8d2a-4c61-9b7e-1a2b3c4d5e6f"

type plannerStub struct {
	got  sip_trunk.CallPlanInput
	plan *sip_trunk.CallPlan
	err  error
	hits int
}

func (s *plannerStub) Plan(_ context.Context, in sip_trunk.CallPlanInput) (*sip_trunk.CallPlan, error) {
	s.hits++
	s.got = in
	return s.plan, s.err
}

func callTools(deps CallDeps) []copilot.Tool {
	history, lines, queues := CallHistoryDeps{}, PhoneLineDeps{}, CallQueueDeps{}
	return []copilot.Tool{
		NewPlaceCallTool(deps),
		NewListCallsTool(history), NewGetCallTool(history),
		NewListPhoneLinesTool(lines), NewCreatePhoneLineTool(lines), NewUpdatePhoneLineTool(lines),
		NewChangePhoneLinePasswordTool(lines), NewDeletePhoneLineTool(lines),
		NewListCallQueuesTool(queues), NewCreateCallQueueTool(queues), NewUpdateCallQueueTool(queues), NewDeleteCallQueueTool(queues),
		NewListTelegramBotsTool(TelegramDeps{}), NewConnectTelegramBotTool(TelegramDeps{}),
	}
}

func TestPlaceCallShowsACardAndNeverDials(t *testing.T) {
	planner := &plannerStub{plan: &sip_trunk.CallPlan{
		PhoneNumber: "+5584999990000",
		Trunks:      []sip_trunk.TrunkChoice{{ID: trunkUUID, Name: "Principal"}},
	}}
	tool := NewPlaceCallTool(CallDeps{Planner: planner})
	cc := copilot.Context{WorkspaceID: "ws-1", UserID: "user-1", SystemAdmin: true}

	res := tool.Execute(context.Background(), cc, map[string]interface{}{"phone_number": "+55 84 99999-0000"})

	if res.Status != copilot.StatusOK || res.Card == nil || res.Card.Kind != copilot.ActionPlaceCall {
		t.Fatalf("result = %+v", res)
	}
	if *res.Card.Call != (copilot.CallIntent{PhoneNumber: "+5584999990000", TrunkID: trunkUUID, TrunkName: "Principal"}) {
		t.Fatalf("call = %+v", *res.Card.Call)
	}
	want := sip_trunk.CallPlanInput{WorkspaceID: "ws-1", UserID: "user-1", IsAdmin: true, PhoneNumber: "+55 84 99999-0000"}
	if planner.got != want {
		t.Fatalf("planned with %+v", planner.got)
	}
	if tool.Meta().Mutating {
		t.Fatal("the card only prepares the call; the member's click places it")
	}
	if m := tool.Meta(); m.Resource != workspace.ResourceSIPTrunks || m.Action != workspace.ActionCall {
		t.Fatalf("meta = %+v", m)
	}
}

func TestPlaceCallListsTheTrunksWhenTheMemberMustChoose(t *testing.T) {
	planner := &plannerStub{plan: &sip_trunk.CallPlan{
		PhoneNumber: "100",
		Trunks:      []sip_trunk.TrunkChoice{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}},
	}}
	res := NewPlaceCallTool(CallDeps{Planner: planner}).Execute(context.Background(), accessCtx, map[string]interface{}{"phone_number": "100"})
	if res.Card.Call.TrunkID != "" {
		t.Fatalf("a trunk was chosen for the member: %+v", res.Card.Call)
	}
	trunks, _ := dataOf(res)["trunks"].([]map[string]string)
	if len(trunks) != 2 || trunks[1]["name"] != "B" || trunks[1]["trunk_id"] != "b" {
		t.Fatalf("trunks = %+v", dataOf(res)["trunks"])
	}
}

func TestPlaceCallRefusesWithoutACard(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status copilot.Status
	}{
		{"no permission", sip_trunk.ErrCallNotPermitted, copilot.StatusDenied},
		{"invalid number", sip_trunk.ErrInvalidPhoneNumber, copilot.StatusError},
		{"no trunk can dial", sip_trunk.ErrNoDialableTrunk, copilot.StatusError},
		{"trunk offline", sip_trunk.ErrTrunkNotRegistered, copilot.StatusError},
		{"unexpected failure", errors.New("db down"), copilot.StatusError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := NewPlaceCallTool(CallDeps{Planner: &plannerStub{err: tc.err}}).Execute(context.Background(), accessCtx, map[string]interface{}{"phone_number": "100"})
			if res.Status != tc.status || res.Card != nil || res.Message == "" {
				t.Fatalf("result = %+v", res)
			}
		})
	}
}

func TestPlaceCallRefusesAnInventedTrunkId(t *testing.T) {
	planner := &plannerStub{}
	res := NewPlaceCallTool(CallDeps{Planner: planner}).Execute(context.Background(), accessCtx, map[string]interface{}{"phone_number": "100", "trunk_id": "tronco-principal"})
	if res.Status != copilot.StatusError || planner.hits != 0 {
		t.Fatalf("result = %+v, planner hits = %d", res, planner.hits)
	}
}
