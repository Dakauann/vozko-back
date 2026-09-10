package crmbulk_usecase

import (
	"context"
	"testing"
)

// A bulk move across funnels is the single most dangerous version of this
// action: one click reorganizes hundreds of conversations onto a board nobody
// was looking at, and there is no undo. It is therefore opt-in per request, the
// same way the single move is, and the default stays refused.
//
// The flag reaches the stage use case unchanged. This service must not decide
// anything about funnels itself: the rule lives in one place, and a second
// implementation here would be a second thing to keep in step.

func TestBulkMoveStageDoesNotCrossFunnelsByDefault(t *testing.T) {
	assigner := &mockStageAssigner{}
	svc := newTestService(assigner)

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws", ActorID: "u1", IsAdmin: true,
		Action:  ActionMoveStage,
		Value:   "stage-b",
		Targets: []EntryRef{{EntryID: "e1", EntryType: "whatsapp"}},
	})
	if res.Forbidden {
		t.Fatalf("the action itself must be permitted: %+v", res)
	}
	if len(assigner.calls) != 1 {
		t.Fatalf("stage assigner calls = %d, want 1", len(assigner.calls))
	}
	if assigner.calls[0].AllowCrossPipeline {
		t.Error("a plain bulk move must leave the funnel guard on")
	}
}

// The deliberate version: the operator confirmed a funnel change for the whole
// selection, so every target carries the authorisation.
func TestBulkMoveStageForwardsTheCrossFunnelOptIn(t *testing.T) {
	assigner := &mockStageAssigner{}
	svc := newTestService(assigner)

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws", ActorID: "u1", IsAdmin: true,
		Action: ActionMoveStage,
		Value:  "stage-b",
		Targets: []EntryRef{
			{EntryID: "e1", EntryType: "whatsapp"},
			{EntryID: "e2", EntryType: "whatsapp"},
		},
		MoveToFunnel: true,
	})
	if res.Forbidden {
		t.Fatalf("the action itself must be permitted: %+v", res)
	}
	if len(assigner.calls) != 2 {
		t.Fatalf("stage assigner calls = %d, want 2", len(assigner.calls))
	}
	for i, call := range assigner.calls {
		if !call.AllowCrossPipeline {
			t.Errorf("target %d did not carry the opt-in", i)
		}
	}
}

// The flag is meaningless outside a stage move, and must not leak into the
// inputs of actions that have nothing to do with funnels.
func TestBulkCrossFunnelOptInIsIgnoredByOtherActions(t *testing.T) {
	assigner := &mockStageAssigner{}
	svc := newTestService(assigner)

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws", ActorID: "u1", IsAdmin: true,
		Action:       ActionAddLabel,
		Value:        "label-1",
		Targets:      []EntryRef{{EntryID: "e1", EntryType: "whatsapp"}},
		MoveToFunnel: true,
	})
	if res.Forbidden {
		t.Fatalf("the action itself must be permitted: %+v", res)
	}
	if len(assigner.calls) != 0 {
		t.Fatalf("a label action reached the stage assigner: %+v", assigner.calls)
	}
}

// newTestService wires the service with the doubles the rest of this package
// already defines, so these tests assert on the stage assigner and nothing else.
func newTestService(sa *mockStageAssigner) *Service {
	return NewService(
		sa,
		&mockLabelAssigner{},
		&mockLabelRemover{},
		&mockEntryAssigner{},
		allowAll(),
		&mockBroadcaster{},
	)
}
