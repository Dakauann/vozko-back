package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/label"
	"vozko/domain/shared"
	"vozko/domain/stage"
)

// The channel registry backs both workspace-wide read paths (inbox list and CRM
// board). These tests pin the contract a channel descriptor must satisfy, so a
// channel added later inherits correct scoping instead of re-deriving it.

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
	want := []shared.EntryType{shared.EntryTypeWhatsApp, shared.EntryTypeInstagram, shared.EntryTypeSupport}
	if len(entrySources) != len(want) {
		t.Fatalf("registry holds %d sources, want %d", len(entrySources), len(want))
	}
	seen := map[shared.EntryType]bool{}
	for _, src := range entrySources {
		if seen[src.EntryType] {
			t.Errorf("duplicate descriptor for %q", src.EntryType)
		}
		seen[src.EntryType] = true

		// Every descriptor must be complete enough to build valid SQL.
		for name, value := range map[string]string{
			"From": src.From, "WorkspaceJoin": src.WorkspaceJoin, "EntryID": src.EntryID,
			"LeadID": src.LeadID, "Account": src.Account, "CreatedAt": src.CreatedAt,
			"UpdatedAt": src.UpdatedAt, "LastMessageAt": src.LastMessageAt, "Deleted": src.Deleted,
		} {
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s: %s must not be empty", src.EntryType, name)
			}
		}
		// The workspace join is what prevents cross-tenant leakage.
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

// Both projections must emit the same column list for every channel, or the
// UNION is rejected by Postgres and the whole inbox/board breaks — including for
// WhatsApp-only tenants.
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
	// A mismatch shifts every later bound value in the UNION, silently corrupting
	// the other channels' filters too.
	if got, want := strings.Count(sql, "?"), len(args); got != want {
		t.Errorf("%d placeholders but %d args:\n%s\nargs=%v", got, want, sql, args)
	}
}

// Every channel must be workspace-scoped through a bound parameter.
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
		// A conversation with no messages must never reach the inbox.
		if !strings.Contains(sql, src.LastMessageAt+" IS NOT NULL") {
			t.Errorf("%s: missing empty-conversation guard:\n%s", src.EntryType, sql)
		}
	}
}

// --- scope selection ---

func TestScopeSelectsChannels(t *testing.T) {
	all := entrySourceScope{}.selected()
	if len(all) != len(entrySources) {
		t.Errorf("an unfiltered scope should read every channel, got %d", len(all))
	}

	only := entrySourceScope{EntryType: shared.EntryTypeInstagram}.selected()
	if len(only) != 1 || only[0].EntryType != shared.EntryTypeInstagram {
		t.Errorf("entry-type scope should select exactly that channel, got %+v", only)
	}

	// A WhatsApp campaign-type filter names a WhatsApp concept, so no other
	// channel can satisfy it.
	campaign := entrySourceScope{WhatsAppCampaignType: "organic"}.selected()
	if len(campaign) != 1 || campaign[0].EntryType != shared.EntryTypeWhatsApp {
		t.Errorf("campaign-type scope should select WhatsApp only, got %+v", campaign)
	}

	// Channels with no status column cannot answer a status filter and are
	// dropped rather than silently matching.
	status := entrySourceScope{ConversationStatus: "finished"}.selected()
	for _, src := range status {
		if src.ConversationStatus == "" {
			t.Errorf("%s has no status column and must be excluded by a status filter", src.EntryType)
		}
	}
	if len(status) == 0 {
		t.Error("a status filter should still select the channels that support it")
	}

	// An unknown channel selects nothing rather than everything.
	if got := (entrySourceScope{EntryType: "telegram"}).selected(); len(got) != 0 {
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

	// An unrecognised campaign-type value must not inject a predicate.
	sql, args = wa.inboxSelect(entrySourceScope{WhatsAppCampaignType: "bogus"}, "ws-1")
	if strings.Contains(sql, wa.CampaignKind+" = ?") {
		t.Errorf("unknown campaign kind should be ignored:\n%s", sql)
	}
	assertPlaceholdersMatchArgs(t, sql, args)
}

// --- scoping guarantees ---

func TestEntrySourceDepartmentScopeFailsClosed(t *testing.T) {
	scope := entrySourceScope{RestrictDepartments: true}

	for _, src := range entrySources {
		if src.DepartmentExempt {
			// An exempt channel is visible to every operator by declaration; the
			// exemption itself is covered by
			// TestDepartmentRestrictionKeepsSupportVisibleButFailsClosedOtherwise.
			continue
		}
		sql, _ := src.inboxSelect(scope, "ws-1")
		// Whether the channel has a department column or not, a restricted
		// operator with no departments must see nothing.
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

	// Support has no department column and never had one filtered, so it stays
	// visible to a department-restricted operator rather than losing them their
	// whole support queue.
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
	// The board's "mine or unassigned" scope must apply to every channel, keyed on
	// that channel's own entry id column.
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

// --- union assembly ---

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

	// Instagram on the board is the capability this registry unlocked: stages,
	// labels and filters all key on (entry_id, entry_type), so appearing here is
	// what makes an Instagram conversation usable on the kanban.
	if !strings.Contains(sql, "'instagram'::text AS entry_type") {
		t.Errorf("board union must include Instagram:\n%s", sql)
	}
}

func TestBuildEntryUnionEmptyWhenNothingSelected(t *testing.T) {
	sql, args := buildEntryUnion(entrySourceScope{EntryType: "telegram"}, "ws-1", entrySource.inboxSelect)
	if sql != "" || len(args) != 0 {
		t.Errorf("an unmatched scope must produce no SQL, got %q / %v", sql, args)
	}
}

// Registering a channel is the whole cost of adding one: the descriptor flows
// into both read paths with no further edits.
func TestRegisteringAChannelReachesBothReadPaths(t *testing.T) {
	const telegram shared.EntryType = "telegram"
	entrySources = append(entrySources, entrySource{
		EntryType:     telegram,
		From:          "telegram_conversations tgc",
		WorkspaceJoin: "JOIN telegram_accounts tga ON tga.id = tgc.account_id AND tga.workspace_id = ?",
		EntryID:       "tgc.id",
		LeadID:        "tgc.contact_id",
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
		if !strings.Contains(sql, "telegram_conversations tgc") {
			t.Errorf("%s path did not pick up the new channel:\n%s", name, sql)
		}
		if !strings.Contains(sql, "'telegram'::text AS entry_type") {
			t.Errorf("%s path missing the new channel's entry_type literal", name)
		}
		assertPlaceholdersMatchArgs(t, sql, args)
	}

	// And it participates in scoping like every other channel.
	if got := (entrySourceScope{EntryType: telegram}).selected(); len(got) != 1 {
		t.Errorf("new channel should be selectable by entry type, got %+v", got)
	}
}

// The registry builds SQL by concatenating identifiers, which is only safe if
// every caller-supplied VALUE is bound rather than interpolated. This asserts
// that property directly: hostile input in each scope field must appear in the
// args, never in the SQL text.
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

// Identifiers come from the registry, which is compile-time data. This pins that
// the SQL text is built only from those constants plus placeholders — the
// invariant that makes the concatenation safe.
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

// --- parity with the hand-written queries this registry replaced ---

// The board must NOT hide finished conversations.
//
// A finished conversation still owns a card in whatever stage it ended in, so
// filtering it out here deletes those cards from the kanban and makes the
// board's own status filter unable to ever select them. The hand-written board
// CTE applied no status predicate; only the inbox list did. Regressed once when
// the default moved into the shared conditions() and both paths inherited it.
func TestBoardKeepsFinishedConversationsAndInboxDoesNot(t *testing.T) {
	board, _ := buildEntryUnion(entrySourceScope{}, "ws-1", entrySource.boardSelect)
	if strings.Contains(board, "IS DISTINCT FROM 'finished'") {
		t.Errorf("board must not filter finished conversations:\n%s", board)
	}

	inbox, _ := buildEntryUnion(entrySourceScope{ExcludeFinished: true}, "ws-1", entrySource.inboxSelect)
	if !strings.Contains(inbox, "IS DISTINCT FROM 'finished'") {
		t.Errorf("inbox default must hide finished conversations:\n%s", inbox)
	}

	// An explicitly named status always wins, on either path.
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

// Support entries were never department-filtered, because they carry no
// department. Failing them closed under a department restriction would take a
// restricted agent's whole support queue away, so support declares itself exempt
// while any other department-less channel still fails closed.
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

	// A department-less channel that has NOT opted out stays hidden.
	entrySources = append(entrySources, entrySource{
		EntryType: "telegram", From: "telegram_conversations tgc",
		WorkspaceJoin: "JOIN telegram_accounts tga ON tga.id = tgc.account_id AND tga.workspace_id = ?",
		EntryID:       "tgc.id", LeadID: "tgc.contact_id", Account: "''",
		CreatedAt: "tgc.created_at", UpdatedAt: "tgc.updated_at",
		LastMessageAt: "tgc.last_message_at", Deleted: "tgc.deleted_at IS NULL",
	})
	t.Cleanup(func() { entrySources = entrySources[:len(entrySources)-1] })

	sql, _ := entrySources[len(entrySources)-1].inboxSelect(scope, "ws-1")
	if !strings.Contains(sql, "1 = 0") {
		t.Errorf("a department-less channel must fail closed unless it opts out:\n%s", sql)
	}
}

// A channel on the board must be able to carry CRM metadata.
//
// Instagram shipped as a card that rendered on the kanban but was rejected by
// the stage and label gates, so it could not be moved or labelled — worse than
// not appearing at all. The board registry and the tagging set are two separate
// lists, so this pins them together rather than trusting them to stay in step.
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
