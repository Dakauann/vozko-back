package stage

import "testing"

func TestAPlacementIsAnEntryOnlyWhenTheStageChanged(t *testing.T) {
	cause := MoveCause{WorkflowID: "wf-a", RunID: "run-1", Chain: 1}
	placed := &EntryStage{ID: "row-2", StageID: "s2", EntryID: "conv-1", EntryType: "whatsapp", WorkspaceID: "ws1", Cause: cause}
	cases := []struct {
		name     string
		placed   *EntryStage
		previous string
		entered  bool
	}{
		{"a conversation with no stage enters one", placed, "", true},
		{"a conversation moved from another stage enters the new one", placed, "s1", true},
		{"placing a conversation where it already is enters nothing", placed, "s2", false},
		{"nothing placed enters nothing", nil, "", false},
		{"a placement without a stage enters nothing", &EntryStage{ID: "row-3", EntryID: "conv-1", WorkspaceID: "ws1"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, entered := EnteredBy(tc.placed, tc.previous)
			if entered != tc.entered {
				t.Fatalf("entered = %v, want %v", entered, tc.entered)
			}
			if !entered {
				return
			}
			want := Entered{MoveID: "row-2", WorkspaceID: "ws1", EntryID: "conv-1", EntryType: "whatsapp", StageID: "s2", FromStageID: tc.previous, Cause: cause}
			if got != want {
				t.Fatalf("entered = %+v, want %+v", got, want)
			}
		})
	}
}
