package attendance_repository

import (
	"strings"

	"vozko/domain/shared"
)

type channelSource struct {
	EntryType shared.EntryType

	EntryTable     string
	EntryAlias     string
	ContainerTable string
	ContainerAlias string
	ContainerJoin  string

	StatusColumn       string
	CloseSourceColumn  string
	CloseOutcomeColumn string
	ClosedAtColumn     string
	DepartmentColumn   string
	ContainerIDColumn  string
	ContainerNameExpr  string
	ContainerNameCols  []string
	WorkspaceColumn    string

	LeadJoin     string
	LeadIDExpr   string
	HasMsgWindow bool
}

var channelSources = []channelSource{
	{
		EntryType:      shared.EntryTypeWhatsApp,
		EntryTable:     "whatsapp_campaign_entries wce",
		EntryAlias:     "wce",
		ContainerTable: "whatsapp_campaigns wc",
		ContainerAlias: "wc",
		ContainerJoin:  "wc.id = wce.campaign_id",

		StatusColumn:       "wce.conversation_status",
		CloseSourceColumn:  "wce.close_source",
		CloseOutcomeColumn: "wce.close_outcome",
		ClosedAtColumn:     "wce.closed_at",
		DepartmentColumn:   "wc.department_id",
		ContainerIDColumn:  "wce.campaign_id",
		ContainerNameExpr:  "NULLIF(wc.name, '')",
		ContainerNameCols:  []string{"wc.name"},
		WorkspaceColumn:    "wc.workspace_id",

		LeadIDExpr:   "COALESCE(wce.lead_id::text, '')",
		HasMsgWindow: true,
	},
	{
		EntryType:      shared.EntryTypeInstagram,
		EntryTable:     "instagram_conversations igc",
		EntryAlias:     "igc",
		ContainerTable: "instagram_accounts iga",
		ContainerAlias: "iga",
		ContainerJoin:  "iga.id = igc.ig_account_id",

		StatusColumn:       "igc.conversation_status",
		CloseSourceColumn:  "igc.close_source",
		CloseOutcomeColumn: "igc.close_outcome",
		ClosedAtColumn:     "igc.closed_at",
		DepartmentColumn:   "iga.department_id",
		ContainerIDColumn:  "igc.ig_account_id",
		ContainerNameExpr:  "COALESCE(NULLIF(iga.username, ''), NULLIF(iga.name, ''))",
		ContainerNameCols:  []string{"iga.username", "iga.name"},
		WorkspaceColumn:    "igc.workspace_id",

		LeadJoin:   "LEFT JOIN instagram_contacts igct ON igct.id = igc.contact_id",
		LeadIDExpr: "COALESCE(igct.lead_id::text, '')",
	},
	{
		EntryType:      shared.EntryTypeTelegram,
		EntryTable:     "telegram_conversations tgc",
		EntryAlias:     "tgc",
		ContainerTable: "telegram_accounts tga",
		ContainerAlias: "tga",
		ContainerJoin:  "tga.id = tgc.account_id",

		StatusColumn:       "tgc.conversation_status",
		CloseSourceColumn:  "tgc.close_source",
		CloseOutcomeColumn: "tgc.close_outcome",
		ClosedAtColumn:     "tgc.closed_at",
		DepartmentColumn:   "tga.department_id",
		ContainerIDColumn:  "tgc.account_id",
		ContainerNameExpr:  "COALESCE(NULLIF(tga.bot_name, ''), NULLIF(tga.bot_username, ''), NULLIF(tga.business_username, ''))",
		ContainerNameCols:  []string{"tga.bot_name", "tga.bot_username", "tga.business_username"},
		WorkspaceColumn:    "tgc.workspace_id",

		LeadJoin:   "LEFT JOIN telegram_contacts tgct ON tgct.id = tgc.contact_id",
		LeadIDExpr: "COALESCE(tgct.lead_id::text, '')",
	},
	{
		EntryType:      shared.EntryTypeUnofficialWhatsApp,
		EntryTable:     "unofficial_whatsapp_conversations uwc",
		EntryAlias:     "uwc",
		ContainerTable: "unofficial_whatsapp_instances uwi",
		ContainerAlias: "uwi",
		ContainerJoin:  "uwi.id = uwc.instance_id",

		StatusColumn:       "uwc.conversation_status",
		CloseSourceColumn:  "uwc.close_source",
		CloseOutcomeColumn: "uwc.close_outcome",
		ClosedAtColumn:     "uwc.closed_at",
		DepartmentColumn:   "uwi.department_id",
		ContainerIDColumn:  "uwc.instance_id",
		ContainerNameExpr:  "COALESCE(NULLIF(uwi.display_name, ''), NULLIF(uwi.profile_name, ''), NULLIF(uwi.provider_name, ''), NULLIF(uwi.phone_number, ''))",
		ContainerNameCols:  []string{"uwi.display_name", "uwi.profile_name", "uwi.provider_name", "uwi.phone_number"},
		WorkspaceColumn:    "uwc.workspace_id",

		LeadJoin:   "LEFT JOIN unofficial_whatsapp_contacts uwct ON uwct.id = uwc.contact_id",
		LeadIDExpr: "COALESCE(uwct.lead_id::text, '')",
	},
}

func selectedChannelSources(channel string) []channelSource {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return channelSources
	}
	for _, src := range channelSources {
		if string(src.EntryType) == channel {
			return []channelSource{src}
		}
	}
	return nil
}

func (s channelSource) statusBucket() string {
	if s.StatusColumn == "" {
		return "'pending'::text"
	}
	return `CASE
					WHEN ` + s.StatusColumn + ` = 'finished' THEN 'finished'
					WHEN ` + s.StatusColumn + ` = 'ongoing' THEN 'ongoing'
					ELSE 'pending'
				END`
}

func (s channelSource) closeSource() string {
	if s.CloseSourceColumn == "" {
		return "''"
	}
	return "COALESCE(" + s.CloseSourceColumn + ", '')"
}

func (s channelSource) closeOutcome() string {
	if s.CloseOutcomeColumn == "" {
		return "''"
	}
	return "COALESCE(" + s.CloseOutcomeColumn + ", '')"
}

func (s channelSource) closedAt() string {
	if s.ClosedAtColumn == "" {
		return "NULL::timestamptz"
	}
	return s.ClosedAtColumn
}

func (s channelSource) containerName() string {
	if s.ContainerNameExpr == "" {
		return "''"
	}
	return "COALESCE(" + s.ContainerNameExpr + ", '')"
}

func (s channelSource) containerNameGroupBy() []string {
	if len(s.ContainerNameCols) > 0 {
		return s.ContainerNameCols
	}
	if s.ContainerNameExpr == "" {
		return nil
	}
	return []string{s.ContainerNameExpr}
}

func (s channelSource) leadID() string {
	if s.LeadIDExpr == "" {
		return "''"
	}
	return s.LeadIDExpr
}

func (s channelSource) projection(isNewContact string) string {
	return s.EntryAlias + `.id AS entry_id, '` + string(s.EntryType) + `'::text AS entry_type,
				` + s.statusBucket() + ` AS status_bucket,
				` + isNewContact + ` AS is_new_contact,
				EXTRACT(HOUR FROM (` + s.EntryAlias + `.created_at))::int AS hour_bucket,
				COALESCE(` + s.DepartmentColumn + `::text, '') AS department_id,
				` + ownerActorIDSQL + ` AS assigned_user_id,
				` + s.EntryAlias + `.created_at,
				` + s.closeSource() + ` AS close_source,
				` + s.closeOutcome() + ` AS close_outcome,
				` + s.closedAt() + ` AS closed_at,
				COALESCE(` + s.ContainerIDColumn + `::text, '') AS container_id,
				` + s.containerName() + ` AS container_name,
				` + s.leadID() + ` AS lead_id`
}

func (s channelSource) groupByColumns() string {
	cols := []string{
		s.EntryAlias + ".id",
		s.EntryAlias + ".created_at",
		s.DepartmentColumn,
		"ia.assigned_user_id",
		"ia.assignee_kind",
		s.ContainerIDColumn,
	}
	if s.StatusColumn != "" {
		cols = append(cols, s.StatusColumn)
	}
	if s.CloseSourceColumn != "" {
		cols = append(cols, s.CloseSourceColumn)
	}
	if s.CloseOutcomeColumn != "" {
		cols = append(cols, s.CloseOutcomeColumn)
	}
	if s.ClosedAtColumn != "" {
		cols = append(cols, s.ClosedAtColumn)
	}
	cols = append(cols, s.containerNameGroupBy()...)
	if s.LeadIDExpr != "" {
		cols = append(cols, s.LeadIDExpr)
	}
	return strings.Join(cols, ", ")
}

func emptyEntryProjection() string {
	return `SELECT NULL::uuid AS entry_id, ''::text AS entry_type, 'pending'::text AS status_bucket,
				FALSE AS is_new_contact, 0 AS hour_bucket, ''::text AS department_id,
				''::text AS assigned_user_id, NOW() AS created_at, ''::text AS close_source,
				''::text AS close_outcome, NULL::timestamptz AS closed_at,
				''::text AS container_id, ''::text AS container_name, ''::text AS lead_id
			WHERE FALSE`
}
