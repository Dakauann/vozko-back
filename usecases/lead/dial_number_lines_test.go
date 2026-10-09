package lead_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/lead"
	"vozko/domain/sip_trunk"
)

func TestNumberLinesPlansTheTrunksForOneChosenNumber(t *testing.T) {
	actor := Actor{UserID: cmdUser, WorkspaceID: cmdWorkspace}
	trunks := &sip_trunk.CallPlan{Trunks: []sip_trunk.TrunkChoice{{ID: "trunk-1", Name: "Matriz"}}}
	cases := []struct {
		name    string
		planner *stubPlanner
		trunks  int
		refusal TrunkRefusal
		err     error
	}{
		{"a line can dial", &stubPlanner{plan: trunks}, 1, "", nil},
		{"the caller may not call", &stubPlanner{err: sip_trunk.ErrCallNotPermitted}, 0, TrunkRefusedNotPermitted, nil},
		{"no line dials the number", &stubPlanner{err: sip_trunk.ErrNoDialableTrunk}, 0, TrunkRefusedNoneDialable, nil},
		{"the number is invalid", &stubPlanner{err: sip_trunk.ErrInvalidPhoneNumber}, 0, "", sip_trunk.ErrInvalidPhoneNumber},
		{"the planner fails", &stubPlanner{err: errors.New("boom")}, 0, "", errors.New("boom")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			targets := dialTargetsWith(t, &dialLeadBook{leads: map[string]*lead.Lead{}}, fakePermissions{}, tc.planner)
			plan, err := targets.NumberLines(context.Background(), actor, "551133334444")
			if tc.err != nil {
				if err == nil || (errors.Is(tc.err, sip_trunk.ErrInvalidPhoneNumber) && !errors.Is(err, sip_trunk.ErrInvalidPhoneNumber)) {
					t.Fatalf("NumberLines = %v, want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Trunks) != tc.trunks || plan.TrunkRefusal != tc.refusal {
				t.Fatalf("plan = %+v", plan)
			}
			if len(tc.planner.asked) != 1 || tc.planner.asked[0].PhoneNumber != "551133334444" || tc.planner.asked[0].UserID != cmdUser {
				t.Fatalf("planner asked %+v", tc.planner.asked)
			}
		})
	}
}

func TestNumberLinesNeedAWorkspaceAndANumber(t *testing.T) {
	targets := dialTargetsWith(t, &dialLeadBook{}, fakePermissions{}, &stubPlanner{})
	if _, err := targets.NumberLines(context.Background(), Actor{UserID: cmdUser}, "551133334444"); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("no workspace = %v", err)
	}
	if _, err := targets.NumberLines(context.Background(), Actor{UserID: cmdUser, WorkspaceID: cmdWorkspace}, " "); !errors.Is(err, sip_trunk.ErrInvalidPhoneNumber) {
		t.Fatalf("no number = %v", err)
	}
}
