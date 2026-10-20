package conversation_repository

import (
	"reflect"
	"strings"
	"testing"

	"github.com/lib/pq"

	"vozko/domain/conversation"
)

func TestAViewerWhoSeesEveryConversationAddsNoPredicate(t *testing.T) {
	clause, args := VisibleEntrySQL("r.entry_id", "r.entry_type", "ws-1", conversation.VisibilityScope{})
	if clause != "" || args != nil {
		t.Fatalf("clause = %q, args = %v", clause, args)
	}
}

func TestTheVisibleEntryPredicateIsTheInboxRulePerChannel(t *testing.T) {
	scope := conversation.VisibilityScope{
		ReadScope:      conversation.ReadScope{DepartmentIDs: []string{"dept-1"}, RestrictDepartments: true, AssigneeOverrideUserID: "user-1"},
		AssignedUserID: "user-1",
	}
	clause, args := VisibleEntrySQL("r.entry_id", "r.entry_type", "ws-1", scope)

	whatsapp := "(r.entry_type = 'whatsapp' AND EXISTS (SELECT 1 FROM whatsapp_campaign_entries wce JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id AND wc.workspace_id = ? WHERE wce.id = r.entry_id" +
		" AND (wc.department_id = ANY(?::uuid[]) OR EXISTS (SELECT 1 FROM inbox_assignments ia_d WHERE ia_d.entry_id = wce.id AND ia_d.assigned_user_id = ?))" +
		" AND (NOT EXISTS (SELECT 1 FROM inbox_assignments ia_s WHERE ia_s.entry_id = wce.id) OR EXISTS (SELECT 1 FROM inbox_assignments ia_s2 WHERE ia_s2.entry_id = wce.id AND ia_s2.assigned_user_id = ?))))"
	if !strings.HasPrefix(clause, "("+whatsapp+" OR ") || !strings.HasSuffix(clause, ")") {
		t.Fatalf("whatsapp branch is not the inbox rule:\n%s", clause)
	}
	for _, src := range entrySources {
		branch := "(r.entry_type = '" + string(src.EntryType) + "' AND EXISTS (SELECT 1 FROM " + src.From + " " + src.WorkspaceJoin + " WHERE " + src.EntryID + " = r.entry_id"
		if !strings.Contains(clause, branch) {
			t.Fatalf("channel %s has no branch:\n%s", src.EntryType, clause)
		}
	}
	if got := strings.Count(clause, "?"); got != len(args) {
		t.Fatalf("placeholders = %d, args = %d", got, len(args))
	}
	if len(args) != 4*len(entrySources) || args[0] != "ws-1" || args[2] != "user-1" || args[3] != "user-1" {
		t.Fatalf("args = %#v", args)
	}
	if !reflect.DeepEqual(args[1], pq.Array([]string{"dept-1"})) {
		t.Fatalf("departments must bind as an array, got %#v", args[1])
	}
}

func TestAViewerWithoutAnyDepartmentSeesNoConversation(t *testing.T) {
	clause, args := VisibleEntrySQL("r.entry_id", "r.entry_type", "ws-1", conversation.NothingVisible())
	if strings.Count(clause, " AND 1 = 0") != len(entrySources) {
		t.Fatalf("every channel must be closed:\n%s", clause)
	}
	if got := strings.Count(clause, "?"); got != len(args) || len(args) != len(entrySources) {
		t.Fatalf("placeholders = %d, args = %d", got, len(args))
	}
}

func TestAMemberWithoutViewOthersSeesOnlyUnassignedAndOwnConversations(t *testing.T) {
	clause, args := VisibleEntrySQL("r.entry_id", "r.entry_type", "ws-1", conversation.VisibilityScope{AssignedUserID: "user-1"})
	if strings.Contains(clause, "department_id") {
		t.Fatalf("no department limit expected:\n%s", clause)
	}
	if strings.Count(clause, "ia_s2.assigned_user_id = ?") != len(entrySources) {
		t.Fatalf("every channel must apply the ownership rule:\n%s", clause)
	}
	if got := strings.Count(clause, "?"); got != len(args) {
		t.Fatalf("placeholders = %d, args = %d", got, len(args))
	}
}

func TestTheInboxListAndTheVisibleEntryPredicateShareOneRule(t *testing.T) {
	scope := entrySourceScope{DepartmentIDs: []string{"dept-1"}, RestrictDepartments: true, AssigneeOverrideUserID: "user-1", AssignedUserID: "user-1"}
	for _, src := range entrySources {
		inbox, inboxArgs := src.conditions(scope)
		rule, ruleArgs := src.visibilityConditions(scope.visibility())
		if !strings.HasSuffix(inbox, rule) || len(ruleArgs) != len(inboxArgs) {
			t.Fatalf("%s: inbox %q does not end with the shared rule %q", src.EntryType, inbox, rule)
		}
	}
}
