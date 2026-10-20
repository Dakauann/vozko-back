package departmentscope

import (
	"reflect"
	"strings"
	"testing"

	"github.com/lib/pq"
)

func TestClause(t *testing.T) {
	cases := []struct {
		name          string
		departmentIDs []string
		restrict      bool
		assignee      string
		entryColumn   string
		wantClause    string
		wantArgs      []interface{}
	}{
		{
			name:        "an unrestricted reader adds nothing",
			restrict:    false,
			entryColumn: "wce.id",
			wantClause:  "",
		},
		{
			name:        "a restricted reader with no department sees nothing",
			restrict:    true,
			entryColumn: "wce.id",
			wantClause:  " AND 1 = 0",
		},
		{
			name:          "a restricted reader sees their departments",
			departmentIDs: []string{"dept-1"},
			restrict:      true,
			entryColumn:   "wce.id",
			wantClause:    " AND wc.department_id = ANY(?::uuid[])",
			wantArgs:      []interface{}{pq.Array([]string{"dept-1"})},
		},
		{
			name:          "a restricted assignee also sees what is assigned to them",
			departmentIDs: []string{"dept-1"},
			restrict:      true,
			assignee:      "user-B",
			entryColumn:   "wce.id",
			wantClause:    " AND (wc.department_id = ANY(?::uuid[]) OR EXISTS (SELECT 1 FROM inbox_assignments ia_d WHERE ia_d.entry_id = wce.id AND ia_d.assigned_user_id = ?))",
			wantArgs:      []interface{}{pq.Array([]string{"dept-1"}), "user-B"},
		},
		{
			name:        "a restricted assignee without departments sees only what is assigned to them",
			restrict:    true,
			assignee:    "user-B",
			entryColumn: "wce.id",
			wantClause:  " AND EXISTS (SELECT 1 FROM inbox_assignments ia_d WHERE ia_d.entry_id = wce.id AND ia_d.assigned_user_id = ?)",
			wantArgs:    []interface{}{"user-B"},
		},
		{
			name:        "an assignee is ignored when the source has no entry column",
			restrict:    true,
			assignee:    "user-B",
			entryColumn: "",
			wantClause:  " AND 1 = 0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clause, args := Clause("wc.department_id", tc.entryColumn, tc.departmentIDs, tc.restrict, tc.assignee)
			if clause != tc.wantClause {
				t.Fatalf("clause = %q, want %q", clause, tc.wantClause)
			}
			if len(args) != len(tc.wantArgs) || (len(args) > 0 && !reflect.DeepEqual(args, tc.wantArgs)) {
				t.Fatalf("args = %#v, want %#v", args, tc.wantArgs)
			}
			if got := strings.Count(clause, "?"); got != len(args) {
				t.Fatalf("placeholders = %d, args = %d", got, len(args))
			}
		})
	}
}
