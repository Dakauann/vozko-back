package conversation_repository

import (
	"strings"

	"vozko/domain/shared"
)

type entrySource struct {
	EntryType shared.EntryType

	From          string
	WorkspaceJoin string

	EntryID string
	Account string

	ConversationStatus string
	CampaignID         string

	CreatedAt     string
	UpdatedAt     string
	LastMessageAt string
	Deleted       string

	Department string

	WhatsAppCampaignScoped bool
	CampaignKind           string
}

func (src entrySource) joinClause(scope entrySourceScope, workspaceID string) (string, []interface{}) {
	join := src.WorkspaceJoin
	args := []interface{}{workspaceID}
	if src.CampaignKind != "" && (scope.WhatsAppCampaignType == "standard" || scope.WhatsAppCampaignType == "organic") {
		join += " AND " + src.CampaignKind + " = ?"
		args = append(args, scope.WhatsAppCampaignType)
	}
	return join, args
}

var entrySources = []entrySource{
	{
		EntryType:     shared.EntryTypeWhatsApp,
		From:          "whatsapp_campaign_entries wce",
		WorkspaceJoin: "JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id AND wc.workspace_id = ?",

		EntryID: "wce.id",

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
		EntryType:     shared.EntryTypeInstagram,
		From:          "instagram_conversations igc",
		WorkspaceJoin: "JOIN instagram_accounts iga ON iga.id = igc.ig_account_id AND iga.workspace_id = ?",

		EntryID: "igc.id",

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
		EntryType:     shared.EntryTypeTelegram,
		From:          "telegram_conversations tgc",
		WorkspaceJoin: "JOIN telegram_accounts tga ON tga.id = tgc.account_id AND tga.workspace_id = ?",

		EntryID: "tgc.id",

		Account: "COALESCE(tgc.account_id::text, '')",

		ConversationStatus: "tgc.conversation_status",
		CampaignID:         "",

		CreatedAt:     "tgc.created_at",
		UpdatedAt:     "tgc.updated_at",
		LastMessageAt: "tgc.last_message_at",
		Deleted:       "tgc.deleted_at IS NULL",

		Department: "tga.department_id",
	},
	{
		EntryType:     shared.EntryTypeUnofficialWhatsApp,
		From:          "unofficial_whatsapp_conversations uwc",
		WorkspaceJoin: "JOIN unofficial_whatsapp_instances uwi ON uwi.id = uwc.instance_id AND uwi.workspace_id = ?",

		EntryID: "uwc.id",
		Account: "COALESCE(uwc.instance_id::text, '')",

		ConversationStatus: "uwc.conversation_status",
		CampaignID: `(SELECT uwcamp.id::text
		              FROM unofficial_whatsapp_campaigns uwcamp
		              WHERE uwcamp.id = NULLIF(uwc.campaign_id, '')::uuid AND uwcamp.deleted_at IS NULL)`,

		CreatedAt:     "uwc.created_at",
		UpdatedAt:     "uwc.updated_at",
		LastMessageAt: "uwc.last_message_at",
		Deleted:       "uwc.deleted_at IS NULL",

		Department: "uwi.department_id",
	},
}

type entrySourceScope struct {
	EntryType            shared.EntryType
	WhatsAppCampaignType string
	ConversationStatus   string
	ExcludeFinished      bool

	DepartmentIDs          []string
	RestrictDepartments    bool
	AssigneeOverrideUserID string
	AssignedUserID         string
}

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

func (src entrySource) conditions(scope entrySourceScope) (string, []interface{}) {
	var sql strings.Builder
	var args []interface{}

	sql.WriteString(" WHERE " + src.Deleted + " AND " + src.LastMessageAt + " IS NOT NULL")

	if src.ConversationStatus != "" {
		if scope.ConversationStatus != "" {
			sql.WriteString(" AND " + src.ConversationStatus + " = ?")
			args = append(args, scope.ConversationStatus)
		} else if scope.ExcludeFinished {
			sql.WriteString(" AND " + src.ConversationStatus + " IS DISTINCT FROM 'finished'")
		}
	}

	if src.Department != "" {
		clause, deptArgs := departmentScopeClause(src.Department, src.EntryID, scope.DepartmentIDs, scope.RestrictDepartments, scope.AssigneeOverrideUserID)
		sql.WriteString(clause)
		args = append(args, deptArgs...)
	} else if scope.RestrictDepartments {
		sql.WriteString(" AND 1 = 0")
	}

	if scope.AssignedUserID != "" {
		clause, selfArgs := assignedSelfClause(src.EntryID, scope.AssignedUserID)
		sql.WriteString(clause)
		args = append(args, selfArgs...)
	}

	return sql.String(), args
}

func (src entrySource) inboxSelect(scope entrySourceScope, workspaceID string) (string, []interface{}) {
	join, args := src.joinClause(scope, workspaceID)
	where, whereArgs := src.conditions(scope)
	sql := "SELECT " + src.EntryID + " AS entry_id, '" + string(src.EntryType) + "'::text AS entry_type, " +
		contactRefUUID(src.EntryType) + " AS lead_id, " +
		src.Account + " AS business_phone_id, " +
		src.LastMessageAt + " AS lm_created_at" +
		" FROM " + src.From + " " + join + where

	return sql, append(args, whereArgs...)
}

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
		contactRefUUID(src.EntryType) + " AS lead_id, " +
		src.Account + " AS business_phone_id, " +
		status + " AS conversation_status, " +
		campaign + " AS campaign_id, " +
		src.CreatedAt + " AS created_at, " +
		src.UpdatedAt + " AS updated_at, " +
		src.LastMessageAt + " AS lm_created_at" +
		" FROM " + src.From + " " + join + where

	return sql, append(args, whereArgs...)
}

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
