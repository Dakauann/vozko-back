package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/label"
	"vozko/domain/shared"
	"vozko/domain/stage"
)

func sourceFor(t *testing.T, entryType shared.EntryType) entrySource {
	t.Helper()
	for _, src := range entrySources {
		if src.EntryType == entryType {
			return src
		}
	}
	t.Fatalf("no registered entry source for %q", entryType)
	return entrySource{}
}

func TestEntrySourcesRegistryCoversEveryChannel(t *testing.T) {
	want := []shared.EntryType{
		shared.EntryTypeWhatsApp,
		shared.EntryTypeInstagram,
		shared.EntryTypeTelegram,
		shared.EntryTypeUnofficialWhatsApp,
		shared.EntryTypeSupport,
	}
	if len(entrySources) != len(want) {
		t.Fatalf("registry holds %d sources, want %d", len(entrySources), len(want))
	}
	seen := map[shared.EntryType]bool{}
	for _, src := range entrySources {
		if seen[src.EntryType] {
			t.Errorf("duplicate descriptor for %q", src.EntryType)
		}
		seen[src.EntryType] = true

		for name, value := range map[string]string{
			"From": src.From, "WorkspaceJoin": src.WorkspaceJoin, "EntryID": src.EntryID,
			"Account": src.Account, "CreatedAt": src.CreatedAt,
			"UpdatedAt": src.UpdatedAt, "LastMessageAt": src.LastMessageAt, "Deleted": src.Deleted,
		} {
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s: %s must not be empty", src.EntryType, name)
			}
		}
		if strings.Count(src.WorkspaceJoin, "?") != 1 {
			t.Errorf("%s: WorkspaceJoin must bind exactly one workspace placeholder", src.EntryType)
		}
	}
	for _, entryType := range want {
		if !seen[entryType] {
			t.Errorf("channel %q is not registered", entryType)
		}
	}
}

func TestEntrySourceProjectionsShareOneShape(t *testing.T) {
	inboxCols := []string{"AS entry_id", "AS entry_type", "AS lead_id", "AS business_phone_id", "AS lm_created_at"}
	boardCols := append([]string{"AS conversation_status", "AS campaign_id", "AS created_at", "AS updated_at"}, inboxCols...)

	for _, src := range entrySources {
		t.Run(string(src.EntryType)+"/inbox", func(t *testing.T) {
			sql, args := src.inboxSelect(entrySourceScope{}, "ws-1")
			for _, col := range inboxCols {
				if !strings.Contains(sql, col) {
					t.Errorf("missing %q:\n%s", col, sql)
				}
			}
			assertPlaceholdersMatchArgs(t, sql, args)
		})
		t.Run(string(src.EntryType)+"/board", func(t *testing.T) {
			sql, args := src.boardSelect(entrySourceScope{}, "ws-1")
			for _, col := range boardCols {
				if !strings.Contains(sql, col) {
					t.Errorf("missing %q:\n%s", col, sql)
				}
			}
			assertPlaceholdersMatchArgs(t, sql, args)
		})
	}
}

func assertPlaceholdersMatchArgs(t *testing.T, sql string, args []interface{}) {
	t.Helper()
	if got, want := strings.Count(sql, "?"), len(args); got != want {
		t.Errorf("%d placeholders but %d args:\n%s\nargs=%v", got, want, sql, args)
	}
}

func TestEntrySourceIsAlwaysWorkspaceScoped(t *testing.T) {
	for _, src := range entrySources {
		sql, args := src.inboxSelect(entrySourceScope{}, "ws-42")
		if len(args) == 0 || args[0] != "ws-42" {
			t.Errorf("%s: workspace must be the first bound arg, got %v", src.EntryType, args)
		}
		if strings.Contains(sql, "ws-42") {
			t.Errorf("%s: workspace id interpolated into SQL:\n%s", src.EntryType, sql)
		}
	}
}

func TestEntrySourceGuardsSoftDeleteAndEmptyConversations(t *testing.T) {
	for _, src := range entrySources {
		sql, _ := src.inboxSelect(entrySourceScope{}, "ws-1")
		if !strings.Contains(sql, src.Deleted) {
			t.Errorf("%s: missing soft-delete guard:\n%s", src.EntryType, sql)
		}
		if !strings.Contains(sql, src.LastMessageAt+" IS NOT NULL") {
			t.Errorf("%s: missing empty-conversation guard:\n%s", src.EntryType, sql)
		}
	}
}

func TestScopeSelectsChannels(t *testing.T) {
	all := entrySourceScope{}.selected()
	if len(all) != len(entrySources) {
		t.Errorf("an unfiltered scope should read every channel, got %d", len(all))
	}

	only := entrySourceScope{EntryType: shared.EntryTypeInstagram}.selected()
	if len(only) != 1 || only[0].EntryType != shared.EntryTypeInstagram {
		t.Errorf("entry-type scope should select exactly that channel, got %+v", only)
	}

	campaign := entrySourceScope{WhatsAppCampaignType: "organic"}.selected()
	if len(campaign) != 1 || campaign[0].EntryType != shared.EntryTypeWhatsApp {
		t.Errorf("campaign-type scope should select WhatsApp only, got %+v", campaign)
	}

	status := entrySourceScope{ConversationStatus: "finished"}.selected()
	for _, src := range status {
		if src.ConversationStatus == "" {
			t.Errorf("%s has no status column and must be excluded by a status filter", src.EntryType)
		}
	}
	if len(status) == 0 {
		t.Error("a status filter should still select the channels that support it")
	}

	if got := (entrySourceScope{EntryType: "messenger"}).selected(); len(got) != 0 {
		t.Errorf("unregistered channel should select nothing, got %+v", got)
	}
}

func TestScopeAppliesCampaignKindOnlyWhereItExists(t *testing.T) {
	wa := sourceFor(t, shared.EntryTypeWhatsApp)
	sql, args := wa.inboxSelect(entrySourceScope{WhatsAppCampaignType: "organic"}, "ws-1")
	if !strings.Contains(sql, wa.CampaignKind+" = ?") {
		t.Errorf("WhatsApp should narrow by campaign kind:\n%s", sql)
	}
	if len(args) < 2 || args[1] != "organic" {
		t.Errorf("campaign kind must be bound right after the workspace, got %v", args)
	}
	assertPlaceholdersMatchArgs(t, sql, args)

	sql, args = wa.inboxSelect(entrySourceScope{WhatsAppCampaignType: "bogus"}, "ws-1")
	if strings.Contains(sql, wa.CampaignKind+" = ?") {
		t.Errorf("unknown campaign kind should be ignored:\n%s", sql)
	}
	assertPlaceholdersMatchArgs(t, sql, args)
}

func TestEntrySourceDepartmentScopeFailsClosed(t *testing.T) {
	scope := entrySourceScope{RestrictDepartments: true}

	for _, src := range entrySources {
		if src.DepartmentExempt {
			continue
		}
		sql, _ := src.inboxSelect(scope, "ws-1")
		if !strings.Contains(sql, "1 = 0") {
			t.Errorf("%s: restricted scope must fail closed:\n%s", src.EntryType, sql)
		}
	}
}

func TestEntrySourceDepartmentScopeBindsDepartments(t *testing.T) {
	scope := entrySourceScope{DepartmentIDs: []string{"d1", "d2"}, RestrictDepartments: true}

	ig := sourceFor(t, shared.EntryTypeInstagram)
	sql, args := ig.inboxSelect(scope, "ws-1")
	if !strings.Contains(sql, ig.Department+" = ANY(?::uuid[])") {
		t.Errorf("Instagram should scope by its account's department:\n%s", sql)
	}
	assertPlaceholdersMatchArgs(t, sql, args)

	sup := sourceFor(t, shared.EntryTypeSupport)
	supSQL, supArgs := sup.inboxSelect(scope, "ws-1")
	if strings.Contains(supSQL, "1 = 0") {
		t.Errorf("support is department-exempt and must stay visible:\n%s", supSQL)
	}
	if strings.Contains(supSQL, "ANY(?::uuid[])") {
		t.Errorf("support has no department column to scope by:\n%s", supSQL)
	}
	assertPlaceholdersMatchArgs(t, supSQL, supArgs)
}

func TestEntrySourceAssignmentScope(t *testing.T) {
	for _, src := range entrySources {
		sql, args := src.boardSelect(entrySourceScope{AssignedUserID: "user-1"}, "ws-1")
		if !strings.Contains(sql, "inbox_assignments") {
			t.Errorf("%s: assignment scope missing:\n%s", src.EntryType, sql)
		}
		if !strings.Contains(sql, src.EntryID) {
			t.Errorf("%s: assignment scope must key on the channel's entry id:\n%s", src.EntryType, sql)
		}
		assertPlaceholdersMatchArgs(t, sql, args)
	}
}

func TestBuildEntryUnionJoinsEverySelectedChannel(t *testing.T) {
	sql, args := buildEntryUnion(entrySourceScope{}, "ws-1", entrySource.boardSelect)
	if got := strings.Count(sql, "UNION ALL"); got != len(entrySources)-1 {
		t.Errorf("expected %d UNION ALL joints, got %d", len(entrySources)-1, got)
	}
	for _, src := range entrySources {
		if !strings.Contains(sql, src.From) {
			t.Errorf("union missing %s", src.From)
		}
	}
	assertPlaceholdersMatchArgs(t, sql, args)

	if !strings.Contains(sql, "'instagram'::text AS entry_type") {
		t.Errorf("board union must include Instagram:\n%s", sql)
	}
}

func TestBuildEntryUnionEmptyWhenNothingSelected(t *testing.T) {
	sql, args := buildEntryUnion(entrySourceScope{EntryType: "messenger"}, "ws-1", entrySource.inboxSelect)
	if sql != "" || len(args) != 0 {
		t.Errorf("an unmatched scope must produce no SQL, got %q / %v", sql, args)
	}
}

func TestRegisteringAChannelReachesBothReadPaths(t *testing.T) {
	const messenger shared.EntryType = "messenger"
	entrySources = append(entrySources, entrySource{
		EntryType:     messenger,
		From:          "messenger_conversations mgc",
		WorkspaceJoin: "JOIN messenger_accounts mga ON mga.id = mgc.account_id AND mga.workspace_id = ?",
		EntryID:       "tgc.id",

		Account:       "COALESCE(tgc.account_id::text, '')",
		CreatedAt:     "tgc.created_at",
		UpdatedAt:     "tgc.updated_at",
		LastMessageAt: "tgc.last_message_at",
		Deleted:       "tgc.deleted_at IS NULL",
		Department:    "tga.department_id",
	})
	t.Cleanup(func() { entrySources = entrySources[:len(entrySources)-1] })

	for name, project := range map[string]func(entrySource, entrySourceScope, string) (string, []interface{}){
		"inbox": entrySource.inboxSelect,
		"board": entrySource.boardSelect,
	} {
		sql, args := buildEntryUnion(entrySourceScope{}, "ws-1", project)
		if !strings.Contains(sql, "messenger_conversations mgc") {
			t.Errorf("%s path did not pick up the new channel:\n%s", name, sql)
		}
		if !strings.Contains(sql, "'messenger'::text AS entry_type") {
			t.Errorf("%s path missing the new channel's entry_type literal", name)
		}
		assertPlaceholdersMatchArgs(t, sql, args)
	}

	if got := (entrySourceScope{EntryType: messenger}).selected(); len(got) != 1 {
		t.Errorf("new channel should be selectable by entry type, got %+v", got)
	}
}

func TestEntrySourceNeverInterpolatesCallerInput(t *testing.T) {
	const inj = "'; DROP TABLE conversation_messages; --"

	scopes := map[string]entrySourceScope{
		"status":       {ConversationStatus: inj},
		"campaignType": {WhatsAppCampaignType: inj},
		"departments":  {DepartmentIDs: []string{inj}, RestrictDepartments: true},
		"assignee":     {AssigneeOverrideUserID: inj, DepartmentIDs: []string{"d1"}, RestrictDepartments: true},
		"assignedUser": {AssignedUserID: inj},
		"entryType":    {EntryType: shared.EntryType(inj)},
	}

	for name, scope := range scopes {
		t.Run(name, func(t *testing.T) {
			for _, project := range []func(entrySource, entrySourceScope, string) (string, []interface{}){
				entrySource.inboxSelect, entrySource.boardSelect,
			} {
				sql, args := buildEntryUnion(scope, inj, project)
				if strings.Contains(sql, "DROP TABLE") {
					t.Fatalf("caller input reached the SQL text:\n%s", sql)
				}
				assertPlaceholdersMatchArgs(t, sql, args)
			}
		})
	}
}

func TestEntrySourceSQLIsBuiltFromRegistryConstantsOnly(t *testing.T) {
	for _, src := range entrySources {
		sql, _ := src.boardSelect(entrySourceScope{
			ConversationStatus: "finished",
			DepartmentIDs:      []string{"11111111-1111-1111-1111-111111111111"},
			AssignedUserID:     "22222222-2222-2222-2222-222222222222",
		}, "33333333-3333-3333-3333-333333333333")

		for _, uuid := range []string{"11111111", "22222222", "33333333"} {
			if strings.Contains(sql, uuid) {
				t.Errorf("%s: a bound value leaked into SQL text (%s):\n%s", src.EntryType, uuid, sql)
			}
		}
	}
}

func TestBoardKeepsFinishedConversationsAndInboxDoesNot(t *testing.T) {
	board, _ := buildEntryUnion(entrySourceScope{}, "ws-1", entrySource.boardSelect)
	if strings.Contains(board, "IS DISTINCT FROM 'finished'") {
		t.Errorf("board must not filter finished conversations:\n%s", board)
	}

	inbox, _ := buildEntryUnion(entrySourceScope{ExcludeFinished: true}, "ws-1", entrySource.inboxSelect)
	if !strings.Contains(inbox, "IS DISTINCT FROM 'finished'") {
		t.Errorf("inbox default must hide finished conversations:\n%s", inbox)
	}

	for name, project := range map[string]func(entrySource, entrySourceScope, string) (string, []interface{}){
		"inbox": entrySource.inboxSelect,
		"board": entrySource.boardSelect,
	} {
		sql, args := buildEntryUnion(
			entrySourceScope{ConversationStatus: "finished", ExcludeFinished: true}, "ws-1", project)
		if strings.Contains(sql, "IS DISTINCT FROM") {
			t.Errorf("%s: explicit status must replace the default, not stack with it:\n%s", name, sql)
		}
		assertPlaceholdersMatchArgs(t, sql, args)
	}
}

func TestDepartmentRestrictionKeepsSupportVisibleButFailsClosedOtherwise(t *testing.T) {
	scope := entrySourceScope{
		DepartmentIDs:       []string{"11111111-1111-1111-1111-111111111111"},
		RestrictDepartments: true,
	}

	for _, src := range entrySources {
		if src.EntryType != shared.EntryTypeSupport {
			continue
		}
		sql, _ := src.inboxSelect(scope, "ws-1")
		if strings.Contains(sql, "1 = 0") {
			t.Errorf("support must stay visible to department-restricted operators:\n%s", sql)
		}
	}

	entrySources = append(entrySources, entrySource{
		EntryType: "messenger", From: "messenger_conversations mgc",
		WorkspaceJoin: "JOIN messenger_accounts mga ON mga.id = mgc.account_id AND mga.workspace_id = ?",
		EntryID:       "tgc.id", Account: "''",
		CreatedAt: "tgc.created_at", UpdatedAt: "tgc.updated_at",
		LastMessageAt: "tgc.last_message_at", Deleted: "tgc.deleted_at IS NULL",
	})
	t.Cleanup(func() { entrySources = entrySources[:len(entrySources)-1] })

	sql, _ := entrySources[len(entrySources)-1].inboxSelect(scope, "ws-1")
	if !strings.Contains(sql, "1 = 0") {
		t.Errorf("a department-less channel must fail closed unless it opts out:\n%s", sql)
	}
}

func TestEveryBoardChannelCanCarryStagesAndLabels(t *testing.T) {
	for _, src := range entrySources {
		if !src.EntryType.SupportsCRMTagging() {
			t.Errorf("%s reaches the board but is not CRM-taggable", src.EntryType)
		}
		if err := stage.ValidateEntryType(string(src.EntryType)); err != nil {
			t.Errorf("%s: stage gate rejects a board channel: %v", src.EntryType, err)
		}
		if err := label.ValidateEntryType(string(src.EntryType)); err != nil {
			t.Errorf("%s: label gate rejects a board channel: %v", src.EntryType, err)
		}
	}
}

func TestCampaignKindReachesBothReadPaths(t *testing.T) {
	for name, project := range map[string]func(entrySource, entrySourceScope, string) (string, []interface{}){
		"inbox": entrySource.inboxSelect,
		"board": entrySource.boardSelect,
	} {
		sql, args := buildEntryUnion(
			entrySourceScope{WhatsAppCampaignType: "organic"}, "ws-1", project)

		if !strings.Contains(sql, "wc.type = ?") {
			t.Errorf("%s: campaign kind must reach the join:\n%s", name, sql)
		}
		for _, other := range []string{"instagram_conversations", "telegram_conversations", "unofficial_whatsapp_conversations"} {
			if strings.Contains(sql, other) {
				t.Errorf("%s: a whatsapp campaign kind must not select %s:\n%s", name, other, sql)
			}
		}
		if len(args) < 2 || args[1] != "organic" {
			t.Errorf("%s: campaign kind must be bound, got args %v", name, args)
		}
		assertPlaceholdersMatchArgs(t, sql, args)
	}
}
