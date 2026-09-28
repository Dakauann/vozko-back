package inbox_assignment

import "testing"

func TestInboxAssignmentVisibility(t *testing.T) {
	cases := []struct {
		name        string
		assignment  *InboxAssignment
		viewer      string
		wantVisible bool
		wantHeldAI  bool
	}{
		{
			name:        "no assignment is visible to everyone",
			assignment:  nil,
			viewer:      "user-1",
			wantVisible: true,
		},
		{
			name:        "assigned to the viewer",
			assignment:  &InboxAssignment{AssignedUserID: "user-1"},
			viewer:      "user-1",
			wantVisible: true,
		},
		{
			name:        "assigned to another operator",
			assignment:  &InboxAssignment{AssignedUserID: "user-2"},
			viewer:      "user-1",
			wantVisible: false,
		},
		{
			name:        "held by the AI is hidden from an operator",
			assignment:  &InboxAssignment{AssignedUserID: "ai:agent-1"},
			viewer:      "user-1",
			wantVisible: false,
			wantHeldAI:  true,
		},
		{
			name:        "the bare agent id is not the AI assignee",
			assignment:  &InboxAssignment{AssignedUserID: "ai:agent-1"},
			viewer:      "agent-1",
			wantVisible: false,
			wantHeldAI:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.assignment.VisibleTo(tc.viewer); got != tc.wantVisible {
				t.Fatalf("VisibleTo = %v, want %v", got, tc.wantVisible)
			}
			if got := tc.assignment.HeldByAutomation(); got != tc.wantHeldAI {
				t.Fatalf("HeldByAutomation = %v, want %v", got, tc.wantHeldAI)
			}
		})
	}
}
