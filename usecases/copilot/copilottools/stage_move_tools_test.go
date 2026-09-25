package copilottools

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/shared"
	"vozko/domain/stage"
)

const (
	knownEntry = "0b0e7c1e-3a7a-4c55-9d7c-2a1c0f1e9b11"
	knownStage = "3c9d8e7f-6a5b-4c3d-8e2f-1a0b9c8d7e6f"
)

type fakeMove struct {
	by    shared.Person
	input stage.AssignEntryStageInput
	calls int
	err   error
}

func (f *fakeMove) Execute(workspaceID string, by shared.Person, in stage.AssignEntryStageInput) (*stage.EntryStage, error) {
	f.calls++
	f.by, f.input = by, in
	if f.err != nil {
		return nil, f.err
	}
	return &stage.EntryStage{StageID: in.StageID, EntryID: in.EntryID}, nil
}

type fakeEntries struct {
	viewer conversation.Viewer
	err    error
}

func (f *fakeEntries) LookupEntry(viewer conversation.Viewer, entryID string, entryType shared.EntryType) (*conversation.InboxEntry, error) {
	f.viewer = viewer
	if f.err != nil {
		return nil, f.err
	}
	return &conversation.InboxEntry{EntryID: entryID, EntryType: string(entryType), LeadName: "Maria", LeadNumber: "5584994409624"}, nil
}

type fakeStageFunnels struct{}

func (fakeStageFunnels) Execute(string) ([]stage.FunnelStages, error) {
	return []stage.FunnelStages{{PipelineID: "p1", PipelineName: "Vendas", Stages: []*stage.Stage{{ID: knownStage, Name: "Proposta"}}}}, nil
}

type fakeStageBroadcast struct{ sent []string }

func (f *fakeStageBroadcast) BroadcastStageUpdate(workspaceID, entryID, entryType string) {
	f.sent = append(f.sent, workspaceID+"|"+entryID+"|"+entryType)
}

func stageMoveDeps(move *fakeMove, entries *fakeEntries, broadcast *fakeStageBroadcast) StageMoveDeps {
	return StageMoveDeps{Move: move, Funnels: fakeStageFunnels{}, Entries: entries, Broadcast: broadcast}
}

func moveArgs() map[string]interface{} {
	return map[string]interface{}{"entry_id": knownEntry, "entry_type": "whatsapp", "stage_id": knownStage}
}

func TestStageMovesAreApprovedChangesWithTheirRoutesPermissions(t *testing.T) {
	deps := stageMoveDeps(&fakeMove{}, &fakeEntries{}, &fakeStageBroadcast{})
	if m := NewMoveConversationStageTool(deps).Meta(); !m.Mutating || m.Resource != "stages" || m.Action != "assign" {
		t.Fatalf("stage move meta = %+v", m)
	}
	if m := NewMoveConversationFunnelTool(deps).Meta(); !m.Mutating || m.Resource != "stages" || m.Action != "transfer" {
		t.Fatalf("funnel move meta = %+v", m)
	}
}

func TestStageMoveDescribesTheConversationAndStageByName(t *testing.T) {
	entries := &fakeEntries{}
	tool := NewMoveConversationStageTool(stageMoveDeps(&fakeMove{}, entries, &fakeStageBroadcast{}))
	fields := tool.(copilot.Describer).Describe(context.Background(), member(), moveArgs())
	got := map[string]string{}
	for _, f := range fields {
		got[f.Key] = f.Value
	}
	if got["conversation"] != "Maria (••••9624)" || got["stage"] != "Proposta (Vendas)" {
		t.Fatalf("fields = %v", fields)
	}
	if entries.viewer.UserID != "u-1" {
		t.Fatalf("looked up as %+v", entries.viewer)
	}
}

func TestStageMoveDescriptionNeverRevealsAConversationTheUserCannotSee(t *testing.T) {
	tool := NewMoveConversationStageTool(stageMoveDeps(&fakeMove{}, &fakeEntries{err: conversation.ErrUnauthorized}, &fakeStageBroadcast{}))
	for _, f := range tool.(copilot.Describer).Describe(context.Background(), member(), moveArgs()) {
		if f.Key == "conversation" && f.Value != unknownConversation {
			t.Fatalf("conversation field = %q", f.Value)
		}
	}
}

func TestStageMoveMovesAsTheUserAndNotifiesTheInbox(t *testing.T) {
	move, broadcast := &fakeMove{}, &fakeStageBroadcast{}
	cc := member()
	cc.SystemAdmin = true
	res := NewMoveConversationStageTool(stageMoveDeps(move, &fakeEntries{}, broadcast)).Execute(context.Background(), cc, moveArgs())
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %s: %s", res.Status, res.Message)
	}
	if move.by != (shared.Person{UserID: "u-1", SystemAdmin: true}) || move.input.AllowCrossPipeline || move.input.StageID != knownStage {
		t.Fatalf("moved by %+v with %+v", move.by, move.input)
	}
	if len(broadcast.sent) != 1 || broadcast.sent[0] != "ws-1|"+knownEntry+"|whatsapp" {
		t.Fatalf("broadcast %v", broadcast.sent)
	}
}

func TestFunnelMoveAllowsCrossingFunnels(t *testing.T) {
	move := &fakeMove{}
	NewMoveConversationFunnelTool(stageMoveDeps(move, &fakeEntries{}, &fakeStageBroadcast{})).Execute(context.Background(), member(), moveArgs())
	if !move.input.AllowCrossPipeline {
		t.Fatal("the funnel move must allow crossing funnels")
	}
}

func TestStageMoveExplainsRefusals(t *testing.T) {
	cases := map[error]copilot.Status{
		stage.ErrEntryAccess:           copilot.StatusDenied,
		stage.ErrStagePipelineMismatch: copilot.StatusError,
		stage.ErrTagNotFound:           copilot.StatusError,
	}
	for err, want := range cases {
		broadcast := &fakeStageBroadcast{}
		res := NewMoveConversationStageTool(stageMoveDeps(&fakeMove{err: err}, &fakeEntries{}, broadcast)).Execute(context.Background(), member(), moveArgs())
		if res.Status != want || len(broadcast.sent) != 0 {
			t.Fatalf("%v: status %s, broadcast %v", err, res.Status, broadcast.sent)
		}
	}
}

func TestStageMoveRefusesInventedIdsBeforeMoving(t *testing.T) {
	move := &fakeMove{}
	for _, args := range []map[string]interface{}{
		{"entry_id": knownEntry, "entry_type": "whatsapp", "stage_id": "proposta"},
		{"entry_id": knownEntry, "entry_type": "fax", "stage_id": knownStage},
		{"entry_type": "whatsapp", "stage_id": knownStage},
	} {
		if res := NewMoveConversationStageTool(stageMoveDeps(move, &fakeEntries{}, &fakeStageBroadcast{})).Execute(context.Background(), member(), args); res.Status != copilot.StatusError {
			t.Fatalf("args %v: status %s", args, res.Status)
		}
	}
	if move.calls != 0 {
		t.Fatal("moved with invented ids")
	}
}
