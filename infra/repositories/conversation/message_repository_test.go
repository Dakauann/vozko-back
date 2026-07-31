package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func TestDepartmentScopeClause_Unrestricted(t *testing.T) {
	clause, args := departmentScopeClause("c.department_id", "ce.id", nil, false, "")
	if clause != "" {
		t.Fatalf("expected empty clause, got %q", clause)
	}
	if len(args) != 0 {
		t.Fatalf("expected no args, got %v", args)
	}
}

func TestDepartmentScopeClause_NoDepartmentsReturnsNoRows(t *testing.T) {
	clause, args := departmentScopeClause("c.department_id", "ce.id", nil, true, "")
	if clause != " AND 1 = 0" {
		t.Fatalf("expected fail-closed clause, got %q", clause)
	}
	if len(args) != 0 {
		t.Fatalf("expected no args, got %v", args)
	}
}

func TestDepartmentScopeClause_RestrictsToExplicitDepartments(t *testing.T) {
	clause, args := departmentScopeClause("c.department_id", "ce.id", []string{"dept-1"}, true, "")
	if clause != " AND c.department_id = ANY(?::uuid[])" {
		t.Fatalf("expected scoped department clause, got %q", clause)
	}
	if len(args) != 1 {
		t.Fatalf("expected one arg, got %d", len(args))
	}
}

func TestDepartmentScopeClause_AssigneeEscape_WithDepartments(t *testing.T) {
	clause, args := departmentScopeClause("wc.department_id", "wce.id", []string{"dept-1"}, true, "user-B")
	if !strings.Contains(clause, "wc.department_id = ANY(?::uuid[])") {
		t.Fatalf("expected dept ANY check in clause, got %q", clause)
	}
	if !strings.Contains(clause, "EXISTS (SELECT 1 FROM inbox_assignments ia_d WHERE ia_d.entry_id = wce.id AND ia_d.assigned_user_id = ?)") {
		t.Fatalf("expected EXISTS escape in clause, got %q", clause)
	}
	if !strings.Contains(clause, " OR ") {
		t.Fatalf("expected OR linkage between dept and assignee escape, got %q", clause)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args (dept slice + user id), got %d: %v", len(args), args)
	}
	if args[len(args)-1] != "user-B" {
		t.Fatalf("expected last arg to be assignee user id, got %v", args[len(args)-1])
	}
}

func TestDepartmentScopeClause_AssigneeEscape_EmptyDepartments(t *testing.T) {
	clause, args := departmentScopeClause("wc.department_id", "wce.id", nil, true, "user-B")
	if strings.Contains(clause, "1 = 0") {
		t.Fatalf("must not fall through to fail-closed when assignee escape is present, got %q", clause)
	}
	if !strings.Contains(clause, "EXISTS (SELECT 1 FROM inbox_assignments ia_d WHERE ia_d.entry_id = wce.id AND ia_d.assigned_user_id = ?)") {
		t.Fatalf("expected EXISTS-only escape, got %q", clause)
	}
	if len(args) != 1 {
		t.Fatalf("expected 1 arg (user id), got %d: %v", len(args), args)
	}
	if args[0] != "user-B" {
		t.Fatalf("expected arg to be assignee user id, got %v", args[0])
	}
}

// The Instagram branch of the workspace inbox union. Its absence is what made a
// received DM vanish on reload — it only existed in the live websocket push.
func TestInstagramWorkspaceEntryCTE_IncludedByDefault(t *testing.T) {
	part, args, include := instagramWorkspaceEntryCTE(conversation.SearchEntriesInput{}, "ws-1")
	if !include {
		t.Fatal("expected Instagram entries in an unfiltered workspace inbox")
	}
	for _, want := range []string{
		"FROM instagram_conversations igc",
		"JOIN instagram_accounts iga",
		"'instagram'::text AS entry_type",
		"igc.contact_id AS lead_id",
		"igc.last_message_at IS NOT NULL",
	} {
		if !strings.Contains(part, want) {
			t.Errorf("CTE missing %q:\n%s", want, part)
		}
	}
	// Default view hides finished conversations, matching the WhatsApp branch.
	if !strings.Contains(part, "IS DISTINCT FROM 'finished'") {
		t.Errorf("expected the default status filter:\n%s", part)
	}
	if len(args) != 1 || args[0] != "ws-1" {
		t.Errorf("expected the workspace id as the only arg, got %v", args)
	}
}

func TestInstagramWorkspaceEntryCTE_ExplicitStatusFilter(t *testing.T) {
	part, args, include := instagramWorkspaceEntryCTE(conversation.SearchEntriesInput{
		ConversationStatus: "finished",
	}, "ws-1")
	if !include {
		t.Fatal("expected Instagram entries when filtering by status")
	}
	if !strings.Contains(part, "igc.conversation_status = ?") {
		t.Errorf("expected an equality status filter:\n%s", part)
	}
	if len(args) != 2 || args[1] != "finished" {
		t.Errorf("expected the status as the second arg, got %v", args)
	}
}

func TestInstagramWorkspaceEntryCTE_ExcludedByChannelScope(t *testing.T) {
	// A WhatsApp-only inbox must not select Instagram conversations.
	if _, _, include := instagramWorkspaceEntryCTE(conversation.SearchEntriesInput{
		EntryType: shared.EntryTypeWhatsApp,
	}, "ws-1"); include {
		t.Error("Instagram must be excluded when the entry type is WhatsApp")
	}
	// A WhatsApp campaign-type filter is meaningless for Instagram.
	if _, _, include := instagramWorkspaceEntryCTE(conversation.SearchEntriesInput{
		WhatsAppCampaignType: "organic",
	}, "ws-1"); include {
		t.Error("Instagram must be excluded when filtering by WhatsApp campaign type")
	}
	// Asking for Instagram explicitly must include it.
	if _, _, include := instagramWorkspaceEntryCTE(conversation.SearchEntriesInput{
		EntryType: shared.EntryTypeInstagram,
	}, "ws-1"); !include {
		t.Error("Instagram must be included when explicitly requested")
	}
}

func TestInstagramWorkspaceEntryCTE_DepartmentScopeFailsClosed(t *testing.T) {
	// A restricted user with no departments must see nothing, not everything.
	part, _, include := instagramWorkspaceEntryCTE(conversation.SearchEntriesInput{
		RestrictDepartments: true,
	}, "ws-1")
	if !include {
		t.Fatal("expected the branch to build")
	}
	if !strings.Contains(part, "1 = 0") {
		t.Errorf("expected a fail-closed department scope:\n%s", part)
	}
}

// --- WhatsApp isolation guarantees for the workspace inbox union ---
//
// The Instagram branch was ADDED to a query WhatsApp tenants already depend on.
// These assert the WhatsApp branch's inputs are untouched and that Instagram
// never widens a WhatsApp-scoped result.

func TestInstagramWorkspaceEntryCTE_NeverSelectsWhatsAppTables(t *testing.T) {
	part, _, include := instagramWorkspaceEntryCTE(conversation.SearchEntriesInput{}, "ws-1")
	if !include {
		t.Fatal("expected the branch to build")
	}
	for _, forbidden := range []string{
		"whatsapp_campaign_entries", "whatsapp_campaigns", "support_entries", "leads",
	} {
		if strings.Contains(part, forbidden) {
			t.Errorf("Instagram branch must not touch %q:\n%s", forbidden, part)
		}
	}
	// Soft-deleted conversations must stay hidden, like every other branch.
	if !strings.Contains(part, "igc.deleted_at IS NULL") {
		t.Errorf("missing soft-delete guard:\n%s", part)
	}
	// A conversation with no messages must not appear in the inbox.
	if !strings.Contains(part, "igc.last_message_at IS NOT NULL") {
		t.Errorf("missing empty-conversation guard:\n%s", part)
	}
}

func TestInstagramWorkspaceEntryCTE_ScopedToOneWorkspace(t *testing.T) {
	// Cross-tenant leakage is the worst failure mode here: the account join must
	// carry the workspace predicate, and the workspace must be a bound arg.
	part, args, _ := instagramWorkspaceEntryCTE(conversation.SearchEntriesInput{}, "ws-42")
	if !strings.Contains(part, "iga.workspace_id = ?") {
		t.Errorf("workspace scope missing from the account join:\n%s", part)
	}
	if len(args) == 0 || args[0] != "ws-42" {
		t.Errorf("workspace id must be the first bound arg, got %v", args)
	}
	// It must be parameterized, never interpolated.
	if strings.Contains(part, "ws-42") {
		t.Errorf("workspace id was interpolated into SQL:\n%s", part)
	}
}

func TestInstagramWorkspaceEntryCTE_ArgOrderMatchesPlaceholders(t *testing.T) {
	// A mismatch between ? count and args silently shifts every later filter in
	// the UNION, which would corrupt the WhatsApp branch's results too.
	cases := []struct {
		name  string
		input conversation.SearchEntriesInput
	}{
		{"defaults", conversation.SearchEntriesInput{}},
		{"status filter", conversation.SearchEntriesInput{ConversationStatus: "finished"}},
		{"departments", conversation.SearchEntriesInput{DepartmentIDs: []string{"d1"}, RestrictDepartments: true}},
		{"departments + status", conversation.SearchEntriesInput{ConversationStatus: "open", DepartmentIDs: []string{"d1"}, RestrictDepartments: true}},
		{"assignee escape", conversation.SearchEntriesInput{DepartmentIDs: []string{"d1"}, RestrictDepartments: true, AssigneeOverrideUserID: "u1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			part, args, include := instagramWorkspaceEntryCTE(tc.input, "ws-1")
			if !include {
				t.Fatal("expected the branch to build")
			}
			if got, want := strings.Count(part, "?"), len(args); got != want {
				t.Errorf("placeholder/arg mismatch: %d placeholders, %d args\n%s", got, want, part)
			}
		})
	}
}

func TestInstagramWorkspaceEntryCTE_ProjectionMatchesUnionShape(t *testing.T) {
	// Every branch of the UNION ALL must project the same four columns in the
	// same order, or Postgres rejects the whole inbox query — WhatsApp included.
	part, _, _ := instagramWorkspaceEntryCTE(conversation.SearchEntriesInput{}, "ws-1")
	for _, col := range []string{
		"AS entry_id", "AS entry_type", "AS lead_id", "AS business_phone_id", "AS lm_created_at",
	} {
		if !strings.Contains(part, col) {
			t.Errorf("projection missing %q:\n%s", col, part)
		}
	}
	// entry_type must be the literal the downstream switch dispatches on.
	if !strings.Contains(part, "'instagram'::text AS entry_type") {
		t.Errorf("entry_type literal wrong:\n%s", part)
	}
}
