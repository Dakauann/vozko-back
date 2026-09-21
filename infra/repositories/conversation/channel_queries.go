package conversation_repository

import (
	"fmt"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type channelQuery struct {
	EntryType shared.EntryType

	EntryTable string

	EntryJoin   string
	ContactJoin string

	AccountIDField     string
	ContainerIDField   string
	ContainerNameField string
	AutomationFields   string

	AutomationColumn string

	StatusColumn string

	ContainerCTE         string
	ContainerCTEEntryCol string

	ContainerFilter    string
	DepartmentColumn   string
	DepartmentEntryCol string

	WindowSubquery string

	CampaignCTE          string
	CampaignCTEEntryCol  string
	CampaignFilter       string
	CampaignDeptColumn   string
	CampaignDeptEntryCol string
}

var channelQueries = []channelQuery{
	{
		EntryType:  shared.EntryTypeWhatsApp,
		EntryTable: "whatsapp_campaign_entries",

		EntryJoin: `JOIN whatsapp_campaign_entries wce ON wce.id = %[1]s AND wce.deleted_at IS NULL
		             JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id`,
		ContactJoin: `JOIN leads l ON l.id = wce.lead_id AND l.deleted_at IS NULL`,

		AccountIDField:     "COALESCE(wc.business_phone_id::text, '')",
		ContainerIDField:   "wc.id::text",
		ContainerNameField: "wc.name",
		AutomationFields: "COALESCE(wc.agent_id::text, '') AS agent_id, " +
			"COALESCE(wc.workflow_id::text, '') AS workflow_id, " +
			"wc.enable_agent_responses AS agent_responses_enabled, " +
			"wc.enable_workflow AS workflow_enabled",

		AutomationColumn: "wce.automation_enabled",
		StatusColumn:     "wce.conversation_status",

		ContainerCTE:         `SELECT wce_f.id AS entry_id FROM whatsapp_campaign_entries wce_f WHERE wce_f.campaign_id = ? AND wce_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "wce_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT wce_f.id FROM whatsapp_campaign_entries wce_f
				JOIN whatsapp_campaigns wc_f ON wc_f.id = wce_f.campaign_id
				WHERE wce_f.campaign_id = ? AND wce_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "wc_f.department_id",
		DepartmentEntryCol: "wce_f.id",

		WindowSubquery: `SELECT wce_w.id FROM whatsapp_campaign_entries wce_w
				JOIN whatsapp_campaigns wc_w ON wc_w.id = wce_w.campaign_id
				JOIN lead_message_windows lmw ON lmw.lead_id = wce_w.lead_id AND lmw.business_phone_id = wc_w.business_phone_id
				WHERE wce_w.deleted_at IS NULL AND lmw.last_message_at > NOW() - INTERVAL '24 hours'`,
	},
	{
		EntryType:  shared.EntryTypeInstagram,
		EntryTable: "instagram_conversations",

		EntryJoin: `JOIN instagram_conversations igc ON igc.id = %[1]s AND igc.deleted_at IS NULL
		             JOIN instagram_accounts iga ON iga.id = igc.ig_account_id`,
		ContactJoin: `JOIN (
	SELECT id, name, username AS number, profile_picture_url, blocked, deleted_at
	FROM instagram_contacts
) l ON l.id = igc.contact_id AND l.deleted_at IS NULL`,

		AccountIDField:     "COALESCE(igc.ig_account_id::text, '')",
		ContainerIDField:   "iga.id::text",
		ContainerNameField: "iga.username",
		AutomationFields: "COALESCE(iga.agent_id::text, '') AS agent_id, " +
			"COALESCE(iga.workflow_id::text, '') AS workflow_id, " +
			"iga.enable_agent_responses AS agent_responses_enabled, " +
			"iga.enable_workflow AS workflow_enabled",

		AutomationColumn: "igc.automation_enabled",
		StatusColumn:     "igc.conversation_status",

		ContainerCTE:         `SELECT igc_f.id AS entry_id FROM instagram_conversations igc_f WHERE igc_f.ig_account_id = ? AND igc_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "igc_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT igc_f.id FROM instagram_conversations igc_f
				JOIN instagram_accounts iga_f ON iga_f.id = igc_f.ig_account_id
				WHERE igc_f.ig_account_id = ? AND igc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "iga_f.department_id",
		DepartmentEntryCol: "igc_f.id",

		WindowSubquery: `SELECT igc_w.id::text FROM instagram_conversations igc_w
				WHERE igc_w.deleted_at IS NULL
				  AND igc_w.last_customer_message_at IS NOT NULL
				  AND igc_w.last_customer_message_at > NOW() - INTERVAL '24 hours'`,
	},
	{
		EntryType:  shared.EntryTypeTelegram,
		EntryTable: "telegram_conversations",

		EntryJoin: `JOIN telegram_conversations tgc ON tgc.id = %[1]s AND tgc.deleted_at IS NULL
		             JOIN telegram_accounts tga ON tga.id = tgc.account_id`,
		ContactJoin: `JOIN (
	SELECT id, COALESCE(NULLIF(TRIM(CONCAT_WS(' ', first_name, last_name)), ''), username) AS name,
	       COALESCE(NULLIF(username, ''), COALESCE(phone_number, '')) AS number,
	       photo_url AS profile_picture_url, blocked, deleted_at
	FROM telegram_contacts
) l ON l.id = tgc.contact_id AND l.deleted_at IS NULL`,

		AccountIDField:     "COALESCE(tgc.account_id::text, '')",
		ContainerIDField:   "tga.id::text",
		ContainerNameField: "tga.bot_username",
		AutomationFields: "COALESCE(tga.agent_id::text, '') AS agent_id, " +
			"COALESCE(tga.workflow_id::text, '') AS workflow_id, " +
			"tga.enable_agent_responses AS agent_responses_enabled, " +
			"tga.enable_workflow AS workflow_enabled",

		AutomationColumn: "tgc.automation_enabled",
		StatusColumn:     "tgc.conversation_status",

		ContainerCTE:         `SELECT tgc_f.id AS entry_id FROM telegram_conversations tgc_f WHERE tgc_f.account_id = ? AND tgc_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "tgc_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT tgc_f.id FROM telegram_conversations tgc_f
				JOIN telegram_accounts tga_f ON tga_f.id = tgc_f.account_id
				WHERE tgc_f.account_id = ? AND tgc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "tga_f.department_id",
		DepartmentEntryCol: "tgc_f.id",

		WindowSubquery: `SELECT tgc_w.id::text FROM telegram_conversations tgc_w
				WHERE tgc_w.deleted_at IS NULL
				  AND tgc_w.last_customer_message_at IS NOT NULL
				  AND tgc_w.last_customer_message_at > NOW() - INTERVAL '24 hours'`,
	},
	{
		EntryType:  shared.EntryTypeUnofficialWhatsApp,
		EntryTable: "unofficial_whatsapp_conversations",

		EntryJoin: `JOIN unofficial_whatsapp_conversations uwc ON uwc.id = %[1]s AND uwc.deleted_at IS NULL
		             JOIN unofficial_whatsapp_instances uwi ON uwi.id = uwc.instance_id
		             LEFT JOIN LATERAL (
		             SELECT uwcamp.id, uwcamp.name, uwcamp.agent_id, uwcamp.workflow_id,
		                    uwcamp.enable_agent_responses, uwcamp.enable_workflow
		             FROM unofficial_whatsapp_campaign_entries uwce
		             JOIN unofficial_whatsapp_campaigns uwcamp
		               ON uwcamp.id = uwce.campaign_id AND uwcamp.deleted_at IS NULL
		             WHERE uwce.conversation_id = uwc.id AND uwce.deleted_at IS NULL
		             ORDER BY uwce.sent_at DESC NULLS LAST, uwce.updated_at DESC
		             LIMIT 1
		         ) camp ON TRUE`,
		ContactJoin: `JOIN (
	SELECT c.id,
	       COALESCE(NULLIF(ld.name, ''), NULLIF(c.contact_name, ''), NULLIF(c.verified_name, ''),
	                NULLIF(c.name, ''), c.phone_number) AS name,
	       c.phone_number AS number,
	       c.picture_url AS profile_picture_url, c.blocked, c.deleted_at
	FROM unofficial_whatsapp_contacts c
	LEFT JOIN leads ld ON ld.id = c.lead_id AND ld.deleted_at IS NULL
) l ON l.id = uwc.contact_id AND l.deleted_at IS NULL`,

		AccountIDField:     "COALESCE(uwc.instance_id::text, '')",
		ContainerIDField:   "COALESCE(camp.id::text, uwi.id::text)",
		ContainerNameField: "COALESCE(camp.name, uwi.display_name)",
		AutomationFields: "CASE WHEN camp.id IS NOT NULL THEN COALESCE(camp.agent_id::text, '') " +
			"ELSE COALESCE(uwi.agent_id::text, '') END AS agent_id, " +
			"CASE WHEN camp.id IS NOT NULL THEN COALESCE(camp.workflow_id::text, '') " +
			"ELSE COALESCE(uwi.workflow_id::text, '') END AS workflow_id, " +
			"CASE WHEN camp.id IS NOT NULL THEN camp.enable_agent_responses " +
			"ELSE uwi.enable_agent_responses END AS agent_responses_enabled, " +
			"CASE WHEN camp.id IS NOT NULL THEN camp.enable_workflow " +
			"ELSE uwi.enable_workflow END AS workflow_enabled",

		AutomationColumn: "uwc.automation_enabled",
		StatusColumn:     "uwc.conversation_status",

		ContainerCTE:         `SELECT uwc_f.id AS entry_id FROM unofficial_whatsapp_conversations uwc_f WHERE uwc_f.instance_id = ? AND uwc_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "uwc_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT uwc_f.id FROM unofficial_whatsapp_conversations uwc_f
				JOIN unofficial_whatsapp_instances uwi_f ON uwi_f.id = uwc_f.instance_id
				WHERE uwc_f.instance_id = ? AND uwc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "uwi_f.department_id",
		DepartmentEntryCol: "uwc_f.id",

		WindowSubquery: "",

		CampaignCTE: `SELECT DISTINCT uwce.conversation_id AS entry_id
			FROM unofficial_whatsapp_campaign_entries uwce
			JOIN unofficial_whatsapp_campaigns uwcamp ON uwcamp.id = uwce.campaign_id AND uwcamp.deleted_at IS NULL
			WHERE uwce.campaign_id = ? AND uwce.deleted_at IS NULL
			  AND uwce.conversation_id IS NOT NULL%[1]s`,
		CampaignCTEEntryCol: "uwce.conversation_id",

		CampaignFilter: `cm.entry_id IN (
				SELECT DISTINCT uwce.conversation_id
				FROM unofficial_whatsapp_campaign_entries uwce
				JOIN unofficial_whatsapp_campaigns uwcamp ON uwcamp.id = uwce.campaign_id AND uwcamp.deleted_at IS NULL
				WHERE uwce.campaign_id = ? AND uwce.deleted_at IS NULL
				  AND uwce.conversation_id IS NOT NULL%[1]s
			)`,
		CampaignDeptColumn:   "uwcamp.department_id",
		CampaignDeptEntryCol: "uwce.conversation_id",
	},
}

type entryRef struct {
	ID   string
	Type shared.EntryType
}

type entryBatch struct {
	EntryType shared.EntryType
	IDs       []string
}

func hydrationBatches(refs []entryRef) []entryBatch {
	byType := make(map[shared.EntryType][]string, len(channelQueries))
	for _, ref := range refs {
		if ref.ID == "" {
			continue
		}
		if _, ok := channelQueryFor(ref.Type); !ok {
			continue
		}
		byType[ref.Type] = append(byType[ref.Type], ref.ID)
	}

	out := make([]entryBatch, 0, len(byType))
	for _, q := range channelQueries {
		if ids := byType[q.EntryType]; len(ids) > 0 {
			out = append(out, entryBatch{EntryType: q.EntryType, IDs: ids})
		}
	}
	return out
}

func channelQueryFor(entryType shared.EntryType) (channelQuery, bool) {
	for _, q := range channelQueries {
		if q.EntryType == entryType {
			return q, true
		}
	}
	return channelQuery{}, false
}

func (q channelQuery) entryJoinOn(entryIDColumn string) string {
	return fmt.Sprintf(q.EntryJoin, entryIDColumn)
}

func (q channelQuery) entryInfoSQL() string {
	return fmt.Sprintf(`
		SELECT %s AS lead_id,
		       %s AS business_phone_id,
		       %s AS campaign_id,
		       %s AS campaign_name,
		       %s,
		       %s AS automation_enabled,
		       %s AS conversation_status
		FROM (SELECT 1) AS entry_anchor
		%s
		LIMIT 1
	`,
		contactRefText(q.EntryType),
		q.AccountIDField,
		q.ContainerIDField,
		q.ContainerNameField,
		q.AutomationFields,
		q.AutomationColumn,
		q.statusColumnOrEmpty(),
		q.entryJoinOn("?::uuid"),
	)
}

func (q channelQuery) statusColumnOrEmpty() string {
	if q.StatusColumn == "" {
		return "''::text"
	}
	return q.StatusColumn
}

func (q channelQuery) containerCTE(assignmentClause string) string {
	return fmt.Sprintf(q.ContainerCTE, assignmentClause)
}

func (q channelQuery) usesCampaignContainer(kind conversation.ContainerKind) bool {
	return kind == conversation.ContainerKindCampaign && q.CampaignCTE != ""
}

func (q channelQuery) cteForKind(kind conversation.ContainerKind, assignmentFor func(string) string) string {
	if q.usesCampaignContainer(kind) {
		return fmt.Sprintf(q.CampaignCTE, assignmentFor(q.CampaignCTEEntryCol))
	}
	return fmt.Sprintf(q.ContainerCTE, assignmentFor(q.ContainerCTEEntryCol))
}

func (q channelQuery) filterForKind(
	kind conversation.ContainerKind,
	departmentClause func(column, entryCol string) (string, []interface{}),
) (string, []interface{}) {
	if q.usesCampaignContainer(kind) {
		clause, args := departmentClause(q.CampaignDeptColumn, q.CampaignDeptEntryCol)
		return fmt.Sprintf(q.CampaignFilter, clause), args
	}
	clause, args := departmentClause(q.DepartmentColumn, q.DepartmentEntryCol)
	return fmt.Sprintf(q.ContainerFilter, clause), args
}

func (q channelQuery) containerFilter(departmentClause string) string {
	return fmt.Sprintf(q.ContainerFilter, departmentClause)
}
