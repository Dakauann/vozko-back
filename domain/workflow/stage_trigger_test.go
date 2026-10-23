package workflow

import (
	"testing"
	"time"
)

func movedBy(workflowID string, chain int) StageEntry {
	return StageEntry{MoveID: "move-1", WorkspaceID: "ws1", EntryID: "conv-1", EntryType: "whatsapp", StageID: "s1", ByWorkflowID: workflowID, ByChain: chain}
}

func TestAStageEntryBecomesAStageAddedEventOnlyWhileTheChainIsBounded(t *testing.T) {
	cases := []struct {
		name      string
		entry     StageEntry
		starts    bool
		wantChain int
		causedBy  string
	}{
		{"a person moving a card starts a fresh chain", movedBy("", 0), true, 0, ""},
		{"a run started by a person moves the card one link further", movedBy("wf-a", 0), true, 1, "wf-a"},
		{"the last allowed link still starts workflows", movedBy("wf-a", MaxStageChain-1), true, MaxStageChain, "wf-a"},
		{"a move past the chain limit starts nothing", movedBy("wf-a", MaxStageChain), false, 0, ""},
		{"a move without its id cannot be made idempotent", StageEntry{WorkspaceID: "ws1", EntryID: "conv-1", EntryType: "whatsapp", StageID: "s1"}, false, 0, ""},
		{"a move without a conversation has nothing to run on", StageEntry{MoveID: "m", WorkspaceID: "ws1", StageID: "s1"}, false, 0, ""},
		{"a move without a stage matches no filter", StageEntry{MoveID: "m", WorkspaceID: "ws1", EntryID: "conv-1", EntryType: "whatsapp"}, false, 0, ""},
		{"a move without a workspace finds no workflows", StageEntry{MoveID: "m", EntryID: "conv-1", EntryType: "whatsapp", StageID: "s1"}, false, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event, starts := StageAddedEvent(tc.entry)
			if starts != tc.starts {
				t.Fatalf("starts = %v, want %v", starts, tc.starts)
			}
			if !starts {
				return
			}
			if event.TriggerType != TriggerStageAdded || event.WorkspaceID != "ws1" || event.EntryID != "conv-1" || event.EntryType != "whatsapp" {
				t.Fatalf("event = %+v", event)
			}
			if event.Data[DataKeyStageID] != "s1" {
				t.Fatalf("data = %v, want the stage the trigger filter compares", event.Data)
			}
			state := NewRunState()
			for k, v := range event.Data {
				state.Set(k, v)
			}
			if got := StageChainOf(state); got != tc.wantChain {
				t.Fatalf("chain carried into the run = %d, want %d", got, tc.wantChain)
			}
			if event.CausedBy != tc.causedBy {
				t.Fatalf("caused by = %q, want %q", event.CausedBy, tc.causedBy)
			}
		})
	}
}

func TestAWorkflowNeverRestartsItselfFromAMoveItMade(t *testing.T) {
	cases := []struct {
		name     string
		causedBy string
		workflow string
		mayStart bool
	}{
		{"a person's move starts any workflow", "", "wf-a", true},
		{"a run's move does not start the workflow that made it", "wf-a", "wf-a", false},
		{"a run's move starts other workflows", "wf-a", "wf-b", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := TriggerEvent{TriggerType: TriggerStageAdded, CausedBy: tc.causedBy}
			if got := event.MayStart(tc.workflow); got != tc.mayStart {
				t.Fatalf("MayStart(%s) = %v, want %v", tc.workflow, got, tc.mayStart)
			}
		})
	}
}

func TestTheSameMoveAlwaysNamesTheSameOccurrence(t *testing.T) {
	first, _ := StageAddedEvent(movedBy("", 0))
	again, _ := StageAddedEvent(movedBy("", 0))
	other := movedBy("", 0)
	other.MoveID = "move-2"
	next, _ := StageAddedEvent(other)
	if first.OccurrenceKey() == "" || first.OccurrenceKey() != again.OccurrenceKey() {
		t.Fatalf("keys = %q and %q, want one stable key per move", first.OccurrenceKey(), again.OccurrenceKey())
	}
	if first.OccurrenceKey() == next.OccurrenceKey() {
		t.Fatal("two moves must not share an occurrence")
	}
	if first.StartsRunPerEvent() {
		t.Fatal("a stage move must still wait for a running run of the same workflow on the conversation")
	}
}

func TestTheChainOfARunIsReadFailingClosed(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
		set   bool
		want  int
	}{
		{"a run no stage move started has no chain", nil, false, 0},
		{"a chain kept in memory", 2, true, 2},
		{"a chain read back from the database", float64(1), true, 1},
		{"a chain that is not a number exhausts the chain", "1", true, MaxStageChain},
		{"a fractional chain exhausts the chain", 1.5, true, MaxStageChain},
		{"a negative chain exhausts the chain", -1, true, MaxStageChain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := NewRunState()
			if tc.set {
				state.Set(StageChainKey, tc.value)
			}
			if got := StageChainOf(state); got != tc.want {
				t.Fatalf("chain = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestTheStageChainNeverReachesTheRecordedTriggerData(t *testing.T) {
	event, _ := StageAddedEvent(movedBy("wf-a", 0))
	state := NewRunState()
	for k, v := range event.Data {
		state.Set(k, v)
	}
	if _, leaked := TriggerDataOf(state)[StageChainKey]; leaked {
		t.Fatal("the chain is engine bookkeeping, not trigger data")
	}
}

func TestTheStageEntryPointShowsWhenAStageMoveLastStartedARun(t *testing.T) {
	kind, ok := EntryKindOfRunOrigin(string(TriggerStageAdded))
	if !ok || kind != EntryStageAdded {
		t.Fatalf("origin kind = %q, %v", kind, ok)
	}
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	w := entryWorkflow(WorkflowStatusActive, true)
	entry := onlyEntry(t, EntryPointsOf(w, []Node{triggerOf(NodeTypeTriggerStageAdded, nil)}, EntryPointFacts{LastStarts: map[EntryPointKind]time.Time{EntryStageAdded: at}}), EntryStageAdded)
	if entry.LastReceivedAt == nil || !entry.LastReceivedAt.Equal(at) || entry.Readiness != EntryReady {
		t.Fatalf("entry = %+v", entry)
	}
}
