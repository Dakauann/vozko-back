package crmbulk_usecase

import (
	"context"
	"testing"

	"vozko/domain/workspace"
)

func grant(perms ...string) *mockAuthorizer {
	allowed := make(map[string]bool, len(perms))
	for _, p := range perms {
		allowed[p] = true
	}
	return &mockAuthorizer{allowPerm: allowed}
}

var (
	permAssign   = string(workspace.ResourceStages) + ":" + string(workspace.ActionAssign)
	permTransfer = string(workspace.ResourceStages) + ":" + string(workspace.ActionTransfer)
)

func serviceWith(sa *mockStageAssigner, authz *mockAuthorizer) *Service {
	return NewService(
		sa,
		&mockLabelAssigner{},
		&mockLabelRemover{},
		&mockEntryAssigner{},
		authz,
		&mockBroadcaster{},
	)
}

func oneTarget() []EntryRef {
	return []EntryRef{{EntryID: "e1", EntryType: "whatsapp"}}
}

func TestBulkMoveStageNeedsOnlyAssignAndKeepsTheFunnelGuard(t *testing.T) {
	assigner := &mockStageAssigner{}
	svc := serviceWith(assigner, grant(permAssign))

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws", ActorID: "u1", IsAdmin: false,
		Action: ActionMoveStage, Value: "stage-b", Targets: oneTarget(),
	})

	if res.Forbidden {
		t.Fatalf("stages:assign must be enough for a plain bulk move: %+v", res)
	}
	if len(assigner.calls) != 1 {
		t.Fatalf("stage assigner calls = %d, want 1", len(assigner.calls))
	}
	if assigner.calls[0].AllowCrossPipeline {
		t.Error("a plain bulk move must leave the funnel guard on")
	}
}

func TestBulkMoveFunnelIsRefusedWithoutTransfer(t *testing.T) {
	assigner := &mockStageAssigner{}
	authz := grant(permAssign)
	svc := serviceWith(assigner, authz)

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws", ActorID: "u1", IsAdmin: false,
		Action: ActionMoveFunnel, Value: "stage-b", Targets: oneTarget(),
	})

	if !res.Forbidden {
		t.Fatal("stages:assign alone must not authorise a cross-funnel bulk move")
	}
	if len(assigner.calls) != 0 {
		t.Fatalf("a refused bulk touched %d targets", len(assigner.calls))
	}
	var askedTransfer bool
	for _, p := range authz.permCalls {
		if p == permTransfer {
			askedTransfer = true
		}
	}
	if !askedTransfer {
		t.Fatalf("stages:transfer was never checked; permCalls = %v", authz.permCalls)
	}
}

func TestBulkMoveFunnelWithTransferAuthorisesEveryTarget(t *testing.T) {
	assigner := &mockStageAssigner{}
	svc := serviceWith(assigner, grant(permAssign, permTransfer))

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws", ActorID: "u1", IsAdmin: false,
		Action: ActionMoveFunnel, Value: "stage-b",
		Targets: []EntryRef{
			{EntryID: "e1", EntryType: "whatsapp"},
			{EntryID: "e2", EntryType: "whatsapp"},
		},
	})

	if res.Forbidden {
		t.Fatalf("stages:transfer must authorise the move: %+v", res)
	}
	if len(assigner.calls) != 2 {
		t.Fatalf("stage assigner calls = %d, want 2", len(assigner.calls))
	}
	for i, call := range assigner.calls {
		if !call.AllowCrossPipeline {
			t.Errorf("target %d did not carry the authorisation", i)
		}
	}
}

func TestBulkMoveStageStillNeedsAssignEvenWithTransfer(t *testing.T) {
	assigner := &mockStageAssigner{}
	svc := serviceWith(assigner, grant(permTransfer))

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws", ActorID: "u1", IsAdmin: false,
		Action: ActionMoveStage, Value: "stage-b", Targets: oneTarget(),
	})

	if !res.Forbidden {
		t.Fatal("move_stage must still require stages:assign")
	}
	if len(assigner.calls) != 0 {
		t.Fatal("a refused bulk must not write")
	}
}

func TestBulkUnknownFunnelishActionIsRefused(t *testing.T) {
	assigner := &mockStageAssigner{}
	svc := serviceWith(assigner, grant(permAssign, permTransfer))

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws", ActorID: "u1", IsAdmin: false,
		Action: "move_to_funnel", Value: "stage-b", Targets: oneTarget(),
	})

	if !res.Forbidden {
		t.Fatal("an unknown action must be refused")
	}
	if len(assigner.calls) != 0 {
		t.Fatal("an unknown action must not write")
	}
}
