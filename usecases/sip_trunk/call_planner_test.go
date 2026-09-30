package sip_trunk_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/sip_trunk"
	"vozko/domain/sip_trunk/siptrunktest"
)

func trunkNamed(id, name string) *sip_trunk.SIPTrunk {
	trunk := ownedTrunk()
	trunk.ID = id
	trunk.Name = name
	return trunk
}

func newPlanner(engine *fakeEngine, trunks ...*sip_trunk.SIPTrunk) *CallPlanner {
	return NewCallPlanner(siptrunktest.NewMemoryRepository(trunks...), engine, grantedCallers{caller + "|" + ownerWorkspace: true})
}

func planInput(number, trunkID string) sip_trunk.CallPlanInput {
	return sip_trunk.CallPlanInput{WorkspaceID: ownerWorkspace, UserID: caller, TrunkID: trunkID, PhoneNumber: number}
}

func TestPlanPicksTheOnlyTrunkThatCanDial(t *testing.T) {
	engine := newFakeEngine()
	engine.markRegistered("ready")
	offline := trunkNamed("offline", "Offline")
	inbound := trunkNamed("inbound", "Inbound")
	inbound.TrunkType = sip_trunk.TrunkTypeInbound
	engine.markRegistered("inbound")

	plan, err := newPlanner(engine, trunkNamed("ready", "Principal"), offline, inbound).Plan(context.Background(), planInput(" +55 (84) 99999-0000 ", ""))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	chosen, ok := plan.Chosen()
	if !ok || chosen != (sip_trunk.TrunkChoice{ID: "ready", Name: "Principal"}) {
		t.Fatalf("chosen = %+v, %v", chosen, ok)
	}
	if plan.PhoneNumber != "5584999990000" {
		t.Fatalf("number = %q", plan.PhoneNumber)
	}
}

func TestPlanLeavesTheChoiceToTheMemberWhenSeveralTrunksCanDial(t *testing.T) {
	engine := newFakeEngine()
	engine.markRegistered("a")
	engine.markRegistered("b")

	plan, err := newPlanner(engine, trunkNamed("a", "A"), trunkNamed("b", "B")).Plan(context.Background(), planInput("100", ""))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, ok := plan.Chosen(); ok || len(plan.Trunks) != 2 {
		t.Fatalf("trunks = %+v", plan.Trunks)
	}
}

func TestPlanRefusesWhatTheCallPathWouldRefuse(t *testing.T) {
	disabled := trunkNamed("disabled", "Off")
	disabled.Enabled = false
	cases := []struct {
		name  string
		input sip_trunk.CallPlanInput
		want  error
	}{
		{"caller without the call permission", func() sip_trunk.CallPlanInput { in := planInput("100", ""); in.UserID = "other"; return in }(), sip_trunk.ErrCallNotPermitted},
		{"another workspace", func() sip_trunk.CallPlanInput {
			in := planInput("100", "")
			in.WorkspaceID = strangerWorkspace
			return in
		}(), sip_trunk.ErrCallNotPermitted},
		{"invalid number", planInput("100@evil", ""), sip_trunk.ErrInvalidPhoneNumber},
		{"unknown trunk", planInput("100", "missing"), sip_trunk.ErrTrunkNotFound},
		{"disabled trunk", planInput("100", "disabled"), sip_trunk.ErrTrunkDisabled},
		{"unregistered trunk", planInput("100", "offline"), sip_trunk.ErrTrunkNotRegistered},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := newFakeEngine()
			_, err := newPlanner(engine, disabled, trunkNamed("offline", "Offline")).Plan(context.Background(), tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPlanSaysSoWhenNoTrunkCanDial(t *testing.T) {
	_, err := newPlanner(newFakeEngine(), trunkNamed("offline", "Offline")).Plan(context.Background(), planInput("100", ""))
	if !errors.Is(err, sip_trunk.ErrNoDialableTrunk) {
		t.Fatalf("err = %v", err)
	}
}

func TestPlanDialsTheNumberAnatelDefinesNotTheLegacyWhatsAppID(t *testing.T) {
	engine := newFakeEngine()
	engine.markRegistered("ready")
	plan, err := newPlanner(engine, trunkNamed("ready", "Principal")).Plan(context.Background(), planInput("558494409684", ""))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.PhoneNumber != "5584994409684" {
		t.Fatalf("number = %q", plan.PhoneNumber)
	}
}

func TestPlanNamesTheRequestedTrunk(t *testing.T) {
	engine := newFakeEngine()
	engine.markRegistered("a")
	engine.markRegistered("b")
	plan, err := newPlanner(engine, trunkNamed("a", "A"), trunkNamed("b", "B")).Plan(context.Background(), planInput("100", "b"))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if chosen, ok := plan.Chosen(); !ok || chosen.Name != "B" {
		t.Fatalf("chosen = %+v", chosen)
	}
}
