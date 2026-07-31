package conversation_repository

import (
	"strings"

	"vozko/domain/shared"
)

// The workspace-wide read paths — the CRM inbox list and the CRM board — both
// answer the same question: "which conversations exist in this workspace?"
// Each channel stores its conversations in its own table, so the answer is a
// UNION whose branches differ only in table names and column expressions.
//
// Those branches used to be written out inline, once per query path. Adding a
// channel therefore meant editing every path, and forgetting one produced
// exactly the bug Instagram shipped with: DMs appeared in the live inbox but
// were missing from the list on reload, and never reached the board at all.
//
// A channel is declared ONCE below. Everything that reads conversations
// workspace-wide — the inbox list, the board, and the stage/label/filter
// machinery layered on top of them — picks it up automatically, because those
// features key on (entry_id, entry_type) and never on a specific table.

// entrySource declares how one channel's conversations are selected.
//
// Column fields are SQL expressions evaluated against From/WorkspaceJoin, so a
// channel can project a literal where it has no equivalent column (support has
// no campaign, Instagram has no business phone).
type entrySource struct {
	EntryType shared.EntryType

	// From is the channel's conversation table with its alias.
	From string
	// WorkspaceJoin scopes rows to one workspace and must contain exactly one
	// placeholder, bound to the workspace id.
	WorkspaceJoin string

	// EntryID identifies the conversation; it becomes entry_id.
	EntryID string
	// LeadID is the contact reference. WhatsApp projects its lead; channels whose
	// contacts are not leads project their own contact id, which the usecase layer
	// resolves per channel.
	LeadID string
	// Account is the channel account that owns the conversation (WhatsApp business
	// phone, Instagram account). Carried in the business_phone_id slot.
	Account string

	// ConversationStatus is the status column, or "" when the channel has none.
	ConversationStatus string
	// CampaignID is the owning campaign, or "" when the channel has no campaigns.
	CampaignID string

	CreatedAt     string
	UpdatedAt     string
	LastMessageAt string
	// Deleted is the soft-delete guard, e.g. "igc.deleted_at IS NULL".
	Deleted string

	// Department is the column carrying the owning department, or "" when the
	// channel is not department-scoped.
	Department string
	// DepartmentExempt lets a channel with no Department column stay visible to
	// department-restricted operators. Without it such a channel is hidden from
	// them, which is the safe default for anything new; support opts out because
	// it has always been visible to everyone in the workspace and hiding it would
	// take a restricted agent's queue away.
	DepartmentExempt bool

	// WhatsAppCampaignScoped marks the channel a WhatsApp campaign-type filter
	// applies to. Every other channel is excluded when that filter is set, since
	// the filter names a WhatsApp concept.
	WhatsAppCampaignScoped bool
	// CampaignKind is the column a campaign-type filter ("standard"/"organic")
	// compares against. Only meaningful together with WhatsAppCampaignScoped.
	CampaignKind string
}

// joinClause returns the workspace join plus any campaign-kind narrowing, with
// its bound arguments in placeholder order.
func (src entrySource) joinClause(scope entrySourceScope, workspaceID string) (string, []interface{}) {
	join := src.WorkspaceJoin
	args := []interface{}{workspaceID}
	if src.CampaignKind != "" && (scope.WhatsAppCampaignType == "standard" || scope.WhatsAppCampaignType == "organic") {
		join += " AND " + src.CampaignKind + " = ?"
		args = append(args, scope.WhatsAppCampaignType)
	}
	return join, args
}

// entrySources is the channel registry.
//
// To add a channel (Telegram, Messenger, …): append its descriptor. No query
// path, usecase or delivery handler changes.
var entrySources = []entrySource{
	{
		EntryType:     shared.EntryTypeWhatsApp,
		From:          "whatsapp_campaign_entries wce",
		WorkspaceJoin: "JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id AND wc.workspace_id = ?",

		EntryID: "wce.id",
		LeadID:  "wce.lead_id",
		Account: "COALESCE(wc.business_phone_id::text, '')",

		ConversationStatus: "wce.conversation_status",
		CampaignID:         "wce.campaign_id::text",

		CreatedAt:     "wce.created_at",
		UpdatedAt:     "wce.updated_at",
		LastMessageAt: "wce.last_message_at",
		Deleted:       "wce.deleted_at IS NULL",

		Department:             "wc.department_id",
		WhatsAppCampaignScoped: true,
		CampaignKind:           "wc.type",
	},
	{
		// The Instagram account plays the container role whatsapp_campaigns plays
		// for WhatsApp: it carries the department and the automation config. There
		// is no campaign, and an Instagram contact is not a lead — the contact id
		// rides the lead slot and the usecase layer resolves it to a name.
		EntryType:     shared.EntryTypeInstagram,
		From:          "instagram_conversations igc",
		WorkspaceJoin: "JOIN instagram_accounts iga ON iga.id = igc.ig_account_id AND iga.workspace_id = ?",

		EntryID: "igc.id",
		LeadID:  "igc.contact_id",
		Account: "COALESCE(igc.ig_account_id::text, '')",

		ConversationStatus: "igc.conversation_status",
		CampaignID:         "",

		CreatedAt:     "igc.created_at",
		UpdatedAt:     "igc.updated_at",
		LastMessageAt: "igc.last_message_at",
		Deleted:       "igc.deleted_at IS NULL",

		Department: "iga.department_id",
	},
	{
		// Support entries have neither campaign nor conversation status; the
		// literals below keep them visible under a default status filter, which
		// uses IS DISTINCT FROM.
		EntryType:     shared.EntryTypeSupport,
		From:          "support_entries se",
		WorkspaceJoin: "JOIN support_inboxes si ON si.id = se.inbox_id AND si.workspace_id = ?",

		EntryID: "se.id",
		LeadID:  "NULL::uuid",
		Account: "''",

		ConversationStatus: "",
		CampaignID:         "",

		CreatedAt:     "se.created_at",
		UpdatedAt:     "se.updated_at",
		LastMessageAt: "se.last_message_at",
		Deleted:       "se.deleted_at IS NULL",

		Department:       "",
		DepartmentExempt: true,
	},
}

// entrySourceScope narrows which channels a query reads.
type entrySourceScope struct {
	// EntryType selects a single channel; empty means every channel.
	EntryType shared.EntryType
	// WhatsAppCampaignType, when set, restricts the read to WhatsApp-campaign
	// channels only.
	WhatsAppCampaignType string
	// ConversationStatus filters on status. Channels without a status column are
	// dropped, since the filter cannot be evaluated for them.
	ConversationStatus string
	// ExcludeFinished hides finished conversations when ConversationStatus names
	// no explicit status.
	//
	// This is the inbox list's default and NOT the board's: on the kanban a
	// finished conversation still owns a card in whatever stage it ended in, and
	// filtering it out here would delete those cards from the board and make the
	// board's own status filter unable to ever select them.
	ExcludeFinished bool

	DepartmentIDs          []string
	RestrictDepartments    bool
	AssigneeOverrideUserID string
	// AssignedUserID applies the board's "assigned to me or unassigned" scope.
	AssignedUserID string
}

// selected returns the channels a scope reads, in registry order.
func (s entrySourceScope) selected() []entrySource {
	out := make([]entrySource, 0, len(entrySources))
	for _, src := range entrySources {
		if s.EntryType != "" && src.EntryType != s.EntryType {
			continue
		}
		if s.WhatsAppCampaignType != "" && !src.WhatsAppCampaignScoped {
			continue
		}
		if s.ConversationStatus != "" && src.ConversationStatus == "" {
			continue
		}
		out = append(out, src)
	}
	return out
}

// conditions builds the per-channel WHERE fragment shared by both projections:
// soft-delete, "has messages", status, department scope and assignment scope.
func (src entrySource) conditions(scope entrySourceScope) (string, []interface{}) {
	var sql strings.Builder
	var args []interface{}

	sql.WriteString(" WHERE " + src.Deleted + " AND " + src.LastMessageAt + " IS NOT NULL")

	if src.ConversationStatus != "" {
		if scope.ConversationStatus != "" {
			sql.WriteString(" AND " + src.ConversationStatus + " = ?")
			args = append(args, scope.ConversationStatus)
		} else if scope.ExcludeFinished {
			// IS DISTINCT FROM keeps rows that never had a status.
			sql.WriteString(" AND " + src.ConversationStatus + " IS DISTINCT FROM 'finished'")
		}
	}

	if src.Department != "" {
		clause, deptArgs := departmentScopeClause(src.Department, src.EntryID, scope.DepartmentIDs, scope.RestrictDepartments, scope.AssigneeOverrideUserID)
		sql.WriteString(clause)
		args = append(args, deptArgs...)
	} else if scope.RestrictDepartments && !src.DepartmentExempt {
		// A department-restricted operator must not see conversations from a
		// channel that has no department to check. Fail closed unless the channel
		// declares itself exempt.
		sql.WriteString(" AND 1 = 0")
	}

	if scope.AssignedUserID != "" {
		clause, selfArgs := assignedSelfClause(src.EntryID, scope.AssignedUserID)
		sql.WriteString(clause)
		args = append(args, selfArgs...)
	}

	return sql.String(), args
}

// inboxSelect projects the five columns the inbox list's union expects.
func (src entrySource) inboxSelect(scope entrySourceScope, workspaceID string) (string, []interface{}) {
	join, args := src.joinClause(scope, workspaceID)
	where, whereArgs := src.conditions(scope)
	sql := "SELECT " + src.EntryID + " AS entry_id, '" + string(src.EntryType) + "'::text AS entry_type, " +
		src.LeadID + " AS lead_id, " +
		src.Account + " AS business_phone_id, " +
		src.LastMessageAt + " AS lm_created_at" +
		" FROM " + src.From + " " + join + where

	return sql, append(args, whereArgs...)
}

// boardSelect projects the nine columns the CRM board's filter compiler reads.
func (src entrySource) boardSelect(scope entrySourceScope, workspaceID string) (string, []interface{}) {
	join, args := src.joinClause(scope, workspaceID)
	where, whereArgs := src.conditions(scope)

	status := "''::text"
	if src.ConversationStatus != "" {
		status = src.ConversationStatus
	}
	campaign := "NULL::text"
	if src.CampaignID != "" {
		campaign = src.CampaignID
	}

	sql := "SELECT " + src.EntryID + " AS entry_id, '" + string(src.EntryType) + "'::text AS entry_type, " +
		src.LeadID + " AS lead_id, " +
		src.Account + " AS business_phone_id, " +
		status + " AS conversation_status, " +
		campaign + " AS campaign_id, " +
		src.CreatedAt + " AS created_at, " +
		src.UpdatedAt + " AS updated_at, " +
		src.LastMessageAt + " AS lm_created_at" +
		" FROM " + src.From + " " + join + where

	return sql, append(args, whereArgs...)
}

// buildEntryUnion joins every selected channel into one UNION ALL, using the
// given per-channel projection.
func buildEntryUnion(
	scope entrySourceScope,
	workspaceID string,
	project func(entrySource, entrySourceScope, string) (string, []interface{}),
) (string, []interface{}) {
	sources := scope.selected()
	if len(sources) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(sources))
	var args []interface{}
	for _, src := range sources {
		sql, srcArgs := project(src, scope, workspaceID)
		parts = append(parts, sql)
		args = append(args, srcArgs...)
	}
	return strings.Join(parts, " UNION ALL "), args
}
