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

	StatusColumn      string
	CloseSourceColumn string
	DepartmentColumn  string
	ContainerIDColumn string
	WorkspaceColumn   string
}

var channelSources = []channelSource{
	{
		EntryType:      shared.EntryTypeWhatsApp,
		EntryTable:     "whatsapp_campaign_entries wce",
		EntryAlias:     "wce",
		ContainerTable: "whatsapp_campaigns wc",
		ContainerAlias: "wc",
		ContainerJoin:  "wc.id = wce.campaign_id",

		StatusColumn:      "wce.conversation_status",
		CloseSourceColumn: "wce.close_source",
		DepartmentColumn:  "wc.department_id",
		ContainerIDColumn: "wce.campaign_id",
		WorkspaceColumn:   "wc.workspace_id",
	},
	{
		EntryType:      shared.EntryTypeInstagram,
		EntryTable:     "instagram_conversations igc",
		EntryAlias:     "igc",
		ContainerTable: "instagram_accounts iga",
		ContainerAlias: "iga",
		ContainerJoin:  "iga.id = igc.ig_account_id",

		StatusColumn:      "igc.conversation_status",
		CloseSourceColumn: "igc.close_source",
		DepartmentColumn:  "iga.department_id",
		ContainerIDColumn: "igc.ig_account_id",
		WorkspaceColumn:   "igc.workspace_id",
	},
	{
		EntryType:      shared.EntryTypeTelegram,
		EntryTable:     "telegram_conversations tgc",
		EntryAlias:     "tgc",
		ContainerTable: "telegram_accounts tga",
		ContainerAlias: "tga",
		ContainerJoin:  "tga.id = tgc.account_id",

		StatusColumn:      "tgc.conversation_status",
		CloseSourceColumn: "tgc.close_source",
		DepartmentColumn:  "tga.department_id",
		ContainerIDColumn: "tgc.account_id",
		WorkspaceColumn:   "tgc.workspace_id",
	},
	{
		EntryType:      shared.EntryTypeUnofficialWhatsApp,
		EntryTable:     "unofficial_whatsapp_conversations uwc",
		EntryAlias:     "uwc",
		ContainerTable: "unofficial_whatsapp_instances uwi",
		ContainerAlias: "uwi",
		ContainerJoin:  "uwi.id = uwc.instance_id",

		StatusColumn:      "uwc.conversation_status",
		CloseSourceColumn: "uwc.close_source",
		DepartmentColumn:  "uwi.department_id",
		ContainerIDColumn: "uwc.instance_id",
		WorkspaceColumn:   "uwc.workspace_id",
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

func (s channelSource) projection(isNewContact string) string {
	return s.EntryAlias + `.id AS entry_id, '` + string(s.EntryType) + `'::text AS entry_type,
				` + s.statusBucket() + ` AS status_bucket,
				` + isNewContact + ` AS is_new_contact,
				EXTRACT(HOUR FROM (` + s.EntryAlias + `.created_at))::int AS hour_bucket,
				COALESCE(` + s.DepartmentColumn + `::text, '') AS department_id,
				COALESCE(ia.assigned_user_id::text, '') AS assigned_user_id,
				` + s.EntryAlias + `.created_at,
				` + s.closeSource() + ` AS close_source`
}

func (s channelSource) groupByColumns() string {
	cols := []string{
		s.EntryAlias + ".id",
		s.EntryAlias + ".created_at",
		s.DepartmentColumn,
		"ia.assigned_user_id",
	}
	if s.StatusColumn != "" {
		cols = append(cols, s.StatusColumn)
	}
	if s.CloseSourceColumn != "" {
		cols = append(cols, s.CloseSourceColumn)
	}
	return strings.Join(cols, ", ")
}
