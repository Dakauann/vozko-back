package conversation_repository

import (
	"fmt"

	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	"vozko/infra/database/sqlexpr"
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

	LineField string

	AutomationColumn string

	StatusColumn string

	CloseTable string

	MessageClockTable string

	ContainerCTE         string
	ContainerCTEEntryCol string

	ContainerFilter    string
	DepartmentColumn   string
	DepartmentEntryCol string

	WindowOpenPredicate string

	GroupPredicate string

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
		LineField: "(SELECT COALESCE(NULLIF(bpl.display_phone_number, ''), bpl.verified_name) FROM whatsapp_business_phone_numbers bpl" +
			" WHERE bpl.id = COALESCE(wce.received_business_phone_id, wc.business_phone_id))",
		AutomationFields: "COALESCE(wc.agent_id::text, '') AS agent_id, " +
			"COALESCE(wc.workflow_id::text, '') AS workflow_id, " +
			"wc.enable_agent_responses AS agent_responses_enabled, " +
			"wc.enable_workflow AS workflow_enabled",

		AutomationColumn:  "wce.automation_enabled",
		StatusColumn:      "wce.conversation_status",
		CloseTable:        "wce",
		MessageClockTable: "wce",

		ContainerCTE:         `SELECT wce_f.id AS entry_id FROM whatsapp_campaign_entries wce_f WHERE wce_f.campaign_id = ? AND wce_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "wce_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT wce_f.id FROM whatsapp_campaign_entries wce_f
				JOIN whatsapp_campaigns wc_f ON wc_f.id = wce_f.campaign_id
				WHERE wce_f.campaign_id = ? AND wce_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "wc_f.department_id",
		DepartmentEntryCol: "wce_f.id",

		WindowOpenPredicate: officialWindowOpen,
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
		LineField:          "COALESCE(NULLIF(iga.username, ''), iga.name)",
		AutomationFields: "COALESCE(iga.agent_id::text, '') AS agent_id, " +
			"COALESCE(iga.workflow_id::text, '') AS workflow_id, " +
			"iga.enable_agent_responses AS agent_responses_enabled, " +
			"iga.enable_workflow AS workflow_enabled",

		AutomationColumn:  "igc.automation_enabled",
		StatusColumn:      "igc.conversation_status",
		CloseTable:        "igc",
		MessageClockTable: "igc",

		ContainerCTE:         `SELECT igc_f.id AS entry_id FROM instagram_conversations igc_f WHERE igc_f.ig_account_id = ? AND igc_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "igc_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT igc_f.id FROM instagram_conversations igc_f
				JOIN instagram_accounts iga_f ON iga_f.id = igc_f.ig_account_id
				WHERE igc_f.ig_account_id = ? AND igc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "iga_f.department_id",
		DepartmentEntryCol: "igc_f.id",

		WindowOpenPredicate: instagramWindowOpen,
	},
	{
		EntryType:  shared.EntryTypeFacebook,
		EntryTable: "facebook_conversations",

		EntryJoin: `JOIN facebook_conversations fbc ON fbc.id = %[1]s AND fbc.deleted_at IS NULL
		             JOIN facebook_pages fbp ON fbp.id = fbc.page_id`,
		ContactJoin: `JOIN (
	SELECT id,
	       COALESCE(NULLIF(name, ''), NULLIF(TRIM(CONCAT_WS(' ', first_name, last_name)), ''), 'Facebook user ' || RIGHT(psid, 4)) AS name,
	       '' AS number, '' AS profile_picture_url, blocked, deleted_at
	FROM facebook_contacts
) l ON l.id = fbc.contact_id AND l.deleted_at IS NULL`,

		AccountIDField:     "COALESCE(fbc.page_id::text, '')",
		ContainerIDField:   "fbp.id::text",
		ContainerNameField: "fbp.name",
		LineField:          "COALESCE(NULLIF(fbp.name, ''), fbp.username)",
		AutomationFields: "COALESCE(fbp.agent_id::text, '') AS agent_id, " +
			"COALESCE(fbp.workflow_id::text, '') AS workflow_id, " +
			"fbp.enable_agent_responses AS agent_responses_enabled, " +
			"fbp.enable_workflow AS workflow_enabled",

		AutomationColumn:  "fbc.automation_enabled",
		StatusColumn:      "fbc.conversation_status",
		CloseTable:        "fbc",
		MessageClockTable: "fbc",

		ContainerCTE:         `SELECT fbc_f.id AS entry_id FROM facebook_conversations fbc_f WHERE fbc_f.page_id = ? AND fbc_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "fbc_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT fbc_f.id FROM facebook_conversations fbc_f
				JOIN facebook_pages fbp_f ON fbp_f.id = fbc_f.page_id
				WHERE fbc_f.page_id = ? AND fbc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "fbp_f.department_id",
		DepartmentEntryCol: "fbc_f.id",

		WindowOpenPredicate: facebookWindowOpen(),
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
		LineField:          "COALESCE(NULLIF(tga.bot_username, ''), tga.bot_name)",
		AutomationFields: "COALESCE(tga.agent_id::text, '') AS agent_id, " +
			"COALESCE(tga.workflow_id::text, '') AS workflow_id, " +
			"tga.enable_agent_responses AS agent_responses_enabled, " +
			"tga.enable_workflow AS workflow_enabled",

		AutomationColumn:  "tgc.automation_enabled",
		StatusColumn:      "tgc.conversation_status",
		CloseTable:        "tgc",
		MessageClockTable: "tgc",

		ContainerCTE:         `SELECT tgc_f.id AS entry_id FROM telegram_conversations tgc_f WHERE tgc_f.account_id = ? AND tgc_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "tgc_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT tgc_f.id FROM telegram_conversations tgc_f
				JOIN telegram_accounts tga_f ON tga_f.id = tgc_f.account_id
				WHERE tgc_f.account_id = ? AND tgc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "tga_f.department_id",
		DepartmentEntryCol: "tgc_f.id",

		WindowOpenPredicate: telegramWindowOpen,
	},
	{
		EntryType:  shared.EntryTypeWebchat,
		EntryTable: "webchat_conversations",

		EntryJoin: `JOIN webchat_conversations wcc ON wcc.id = %[1]s AND wcc.deleted_at IS NULL
		             JOIN webchat_widgets wcw ON wcw.id = wcc.widget_id`,
		ContactJoin: `JOIN (
	SELECT id, COALESCE(NULLIF(name, ''), NULLIF(email, ''), '') AS name,
	       COALESCE(NULLIF(phone, ''), COALESCE(email, '')) AS number,
	       '' AS profile_picture_url, blocked, deleted_at
	FROM webchat_visitors
) l ON l.id = wcc.visitor_id AND l.deleted_at IS NULL`,

		AccountIDField:     "COALESCE(wcc.widget_id::text, '')",
		ContainerIDField:   "wcw.id::text",
		ContainerNameField: "wcw.name",
		LineField:          "wcw.name",
		AutomationFields: "COALESCE(wcw.agent_id::text, '') AS agent_id, " +
			"COALESCE(wcw.workflow_id::text, '') AS workflow_id, " +
			"wcw.enable_agent_responses AS agent_responses_enabled, " +
			"wcw.enable_workflow AS workflow_enabled",

		AutomationColumn:  "wcc.automation_enabled",
		StatusColumn:      "wcc.conversation_status",
		CloseTable:        "wcc",
		MessageClockTable: "wcc",

		ContainerCTE:         `SELECT wcc_f.id AS entry_id FROM webchat_conversations wcc_f WHERE wcc_f.widget_id = ? AND wcc_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "wcc_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT wcc_f.id FROM webchat_conversations wcc_f
				JOIN webchat_widgets wcw_f ON wcw_f.id = wcc_f.widget_id
				WHERE wcc_f.widget_id = ? AND wcc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "wcw_f.department_id",
		DepartmentEntryCol: "wcc_f.id",

		WindowOpenPredicate: webchatWindowOpen,
	},
	{
		EntryType:  shared.EntryTypeMercadoLivre,
		EntryTable: "mercadolivre_conversations",

		EntryJoin: `JOIN mercadolivre_conversations mlc ON mlc.id = %[1]s AND mlc.deleted_at IS NULL
		             JOIN mercadolivre_accounts mla ON mla.id = mlc.account_id`,
		ContactJoin: `JOIN (
	SELECT id, COALESCE(NULLIF(nickname, ''), ml_user_id) AS name,
	       COALESCE(NULLIF(nickname, ''), ml_user_id) AS number,
	       '' AS profile_picture_url, false AS blocked, deleted_at
	FROM mercadolivre_contacts
) l ON l.id = mlc.contact_id AND l.deleted_at IS NULL`,

		AccountIDField:     "COALESCE(mlc.account_id::text, '')",
		ContainerIDField:   "mla.id::text",
		ContainerNameField: mldomain.AccountNameSQL("mla."),
		LineField:          "COALESCE(NULLIF(mla.nickname, ''), mla.ml_user_id)",
		AutomationFields: "COALESCE(mla.agent_id::text, '') AS agent_id, " +
			"COALESCE(mla.workflow_id::text, '') AS workflow_id, " +
			"mla.enable_agent_responses AS agent_responses_enabled, " +
			"mla.enable_workflow AS workflow_enabled",

		AutomationColumn:  "mlc.automation_enabled",
		StatusColumn:      "mlc.conversation_status",
		CloseTable:        "mlc",
		MessageClockTable: "mlc",

		ContainerCTE:         `SELECT mlc_f.id AS entry_id FROM mercadolivre_conversations mlc_f WHERE mlc_f.account_id = ? AND mlc_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "mlc_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT mlc_f.id FROM mercadolivre_conversations mlc_f
				JOIN mercadolivre_accounts mla_f ON mla_f.id = mlc_f.account_id
				WHERE mlc_f.account_id = ? AND mlc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "mla_f.department_id",
		DepartmentEntryCol: "mlc_f.id",

		WindowOpenPredicate: mercadoLivreWindowOpen,
	},
	{
		EntryType:  shared.EntryTypeTikTok,
		EntryTable: "tiktok_conversations",

		EntryJoin: `JOIN tiktok_conversations ttc ON ttc.id = %[1]s AND ttc.deleted_at IS NULL
		             JOIN tiktok_accounts tta ON tta.id = ttc.account_id`,
		ContactJoin: `JOIN (
	SELECT id, COALESCE(NULLIF(display_name, ''), NULLIF(username, ''), unique_identifier) AS name,
	       COALESCE(NULLIF(username, ''), unique_identifier) AS number,
	       '' AS profile_picture_url, false AS blocked, deleted_at
	FROM tiktok_contacts
) l ON l.id = ttc.contact_id AND l.deleted_at IS NULL`,

		AccountIDField:     "COALESCE(ttc.account_id::text, '')",
		ContainerIDField:   "tta.id::text",
		ContainerNameField: "tta.username",
		LineField:          "COALESCE(NULLIF(tta.username, ''), NULLIF(tta.display_name, ''), tta.open_id)",
		AutomationFields: "COALESCE(tta.agent_id::text, '') AS agent_id, " +
			"COALESCE(tta.workflow_id::text, '') AS workflow_id, " +
			"tta.enable_agent_responses AS agent_responses_enabled, " +
			"tta.enable_workflow AS workflow_enabled",

		AutomationColumn:  "ttc.automation_enabled",
		StatusColumn:      "ttc.conversation_status",
		CloseTable:        "ttc",
		MessageClockTable: "ttc",

		ContainerCTE:         `SELECT ttc_f.id AS entry_id FROM tiktok_conversations ttc_f WHERE ttc_f.account_id = ? AND ttc_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "ttc_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT ttc_f.id FROM tiktok_conversations ttc_f
				JOIN tiktok_accounts tta_f ON tta_f.id = ttc_f.account_id
				WHERE ttc_f.account_id = ? AND ttc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "tta_f.department_id",
		DepartmentEntryCol: "ttc_f.id",

		WindowOpenPredicate: tiktokWindowOpen,
	},
	{
		EntryType:  shared.EntryTypeEmail,
		EntryTable: "email_conversations",

		EntryJoin: `JOIN email_conversations emc ON emc.id = %[1]s AND emc.deleted_at IS NULL
		             JOIN email_inboxes emi ON emi.id = emc.sender_id`,
		ContactJoin: `JOIN (
	SELECT id, COALESCE(NULLIF(display_name, ''), address) AS name,
	       address AS number,
	       '' AS profile_picture_url, false AS blocked, deleted_at
	FROM email_contacts
) l ON l.id = emc.contact_id AND l.deleted_at IS NULL`,

		AccountIDField:     "COALESCE(emc.sender_id::text, '')",
		ContainerIDField:   "emi.id::text",
		ContainerNameField: "COALESCE(NULLIF(emi.name, ''), emi.address)",
		LineField:          "COALESCE(NULLIF(emi.address, ''), emi.name)",
		AutomationFields: "COALESCE(emi.agent_id::text, '') AS agent_id, " +
			"COALESCE(emi.workflow_id::text, '') AS workflow_id, " +
			"emi.enable_agent_responses AS agent_responses_enabled, " +
			"emi.enable_workflow AS workflow_enabled",

		AutomationColumn:  "emc.automation_enabled",
		StatusColumn:      "emc.conversation_status",
		CloseTable:        "emc",
		MessageClockTable: "emc",

		ContainerCTE:         `SELECT emc_f.id AS entry_id FROM email_conversations emc_f WHERE emc_f.sender_id = ? AND emc_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "emc_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT emc_f.id FROM email_conversations emc_f
				JOIN email_inboxes emi_f ON emi_f.id = emc_f.sender_id
				WHERE emc_f.sender_id = ? AND emc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "emi_f.department_id",
		DepartmentEntryCol: "emc_f.id",

		WindowOpenPredicate: emailWindowOpen,
	},
	{
		EntryType:  shared.EntryTypeRCS,
		EntryTable: "rcs_conversations",

		EntryJoin: `JOIN rcs_conversations rcc ON rcc.id = %[1]s AND rcc.deleted_at IS NULL
		             JOIN rcs_senders rcsd ON rcsd.id = rcc.sender_id`,
		ContactJoin: `JOIN (
	SELECT id, COALESCE(NULLIF(display_name, ''), phone) AS name,
	       phone AS number,
	       '' AS profile_picture_url, false AS blocked, deleted_at
	FROM carrier_contacts
) l ON l.id = rcc.contact_id AND l.deleted_at IS NULL`,

		AccountIDField:     "COALESCE(rcc.sender_id::text, '')",
		ContainerIDField:   "rcsd.id::text",
		ContainerNameField: "COALESCE(NULLIF(rcsd.brand_name, ''), rcsd.id::text)",
		LineField:          "COALESCE(NULLIF(rcsd.brand_name, ''), rcsd.id::text)",
		AutomationFields: "COALESCE(rcsd.agent_id::text, '') AS agent_id, " +
			"COALESCE(rcsd.workflow_id::text, '') AS workflow_id, " +
			"rcsd.enable_agent_responses AS agent_responses_enabled, " +
			"rcsd.enable_workflow AS workflow_enabled",

		AutomationColumn:  "rcc.automation_enabled",
		StatusColumn:      "rcc.conversation_status",
		CloseTable:        "rcc",
		MessageClockTable: "rcc",

		ContainerCTE:         `SELECT rcc_f.id AS entry_id FROM rcs_conversations rcc_f WHERE rcc_f.sender_id = ? AND rcc_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "rcc_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT rcc_f.id FROM rcs_conversations rcc_f
				JOIN rcs_senders rcsd_f ON rcsd_f.id = rcc_f.sender_id
				WHERE rcc_f.sender_id = ? AND rcc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "rcsd_f.department_id",
		DepartmentEntryCol: "rcc_f.id",

		WindowOpenPredicate: rcsWindowOpen,
	},
	{
		EntryType:  shared.EntryTypeSMS,
		EntryTable: "sms_conversations",

		EntryJoin: `JOIN sms_conversations smc ON smc.id = %[1]s AND smc.deleted_at IS NULL
		             JOIN sms_routes smr ON smr.id = smc.sender_id`,
		ContactJoin: `JOIN (
	SELECT id, COALESCE(NULLIF(display_name, ''), phone) AS name,
	       phone AS number,
	       '' AS profile_picture_url, false AS blocked, deleted_at
	FROM carrier_contacts
) l ON l.id = smc.contact_id AND l.deleted_at IS NULL`,

		AccountIDField:     "COALESCE(smc.sender_id::text, '')",
		ContainerIDField:   "smr.id::text",
		ContainerNameField: "COALESCE(NULLIF(smr.brand_prefix, ''), smr.id::text)",
		LineField:          "COALESCE(NULLIF(smr.number, ''), smr.brand_prefix)",
		AutomationFields: "COALESCE(smr.agent_id::text, '') AS agent_id, " +
			"COALESCE(smr.workflow_id::text, '') AS workflow_id, " +
			"smr.enable_agent_responses AS agent_responses_enabled, " +
			"smr.enable_workflow AS workflow_enabled",

		AutomationColumn:  "smc.automation_enabled",
		StatusColumn:      "smc.conversation_status",
		CloseTable:        "smc",
		MessageClockTable: "smc",

		ContainerCTE:         `SELECT smc_f.id AS entry_id FROM sms_conversations smc_f WHERE smc_f.sender_id = ? AND smc_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "smc_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT smc_f.id FROM sms_conversations smc_f
				JOIN sms_routes smr_f ON smr_f.id = smc_f.sender_id
				WHERE smc_f.sender_id = ? AND smc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "smr_f.department_id",
		DepartmentEntryCol: "smc_f.id",

		WindowOpenPredicate: smsWindowOpen,
	},
	{
		EntryType:  shared.EntryTypeUnofficialWhatsApp,
		EntryTable: "unofficial_whatsapp_conversations",

		EntryJoin: `JOIN unofficial_whatsapp_conversations uwc ON uwc.id = %[1]s AND uwc.deleted_at IS NULL
		             JOIN unofficial_whatsapp_instances uwi ON uwi.id = uwc.instance_id
		             ` + UnofficialCampaignJoin("uwc", "camp"),
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
		LineField:          "COALESCE(NULLIF(uwi.display_name, ''), uwi.phone_number)",
		AutomationFields: "CASE WHEN camp.id IS NOT NULL THEN COALESCE(camp.agent_id::text, '') " +
			"ELSE COALESCE(uwi.agent_id::text, '') END AS agent_id, " +
			"CASE WHEN camp.id IS NOT NULL THEN COALESCE(camp.workflow_id::text, '') " +
			"ELSE COALESCE(uwi.workflow_id::text, '') END AS workflow_id, " +
			"CASE WHEN camp.id IS NOT NULL THEN camp.enable_agent_responses " +
			"ELSE uwi.enable_agent_responses END AS agent_responses_enabled, " +
			"CASE WHEN camp.id IS NOT NULL THEN camp.enable_workflow " +
			"ELSE uwi.enable_workflow END AS workflow_enabled",

		AutomationColumn:  "uwc.automation_enabled",
		StatusColumn:      "uwc.conversation_status",
		CloseTable:        "uwc",
		MessageClockTable: "uwc",

		ContainerCTE:         `SELECT uwc_f.id AS entry_id FROM unofficial_whatsapp_conversations uwc_f WHERE uwc_f.instance_id = ? AND uwc_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "uwc_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT uwc_f.id FROM unofficial_whatsapp_conversations uwc_f
				JOIN unofficial_whatsapp_instances uwi_f ON uwi_f.id = uwc_f.instance_id
				WHERE uwc_f.instance_id = ? AND uwc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "uwi_f.department_id",
		DepartmentEntryCol: "uwc_f.id",

		WindowOpenPredicate: unofficialWindowOpen,
		GroupPredicate:      unofficialGroup,

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

func (q channelQuery) entryInfoQuery(entryID string) (string, []any) {
	return q.entryInfoSQL(), []any{entryID, entryID, string(q.EntryType)}
}

func (q channelQuery) entryInfoSQL() string {
	return fmt.Sprintf(`
		SELECT %s AS lead_id,
		       %s AS business_phone_id,
		       %s AS campaign_id,
		       %s AS campaign_name,
		       %s,
		       %s AS automation_enabled,
		       %s AS conversation_status,
		       %s,
		       %s,
		       COALESCE(cd.automation_kind, '') AS delegate_kind,
		       COALESCE(cd.automation_id, '') AS delegate_id
		FROM (SELECT 1) AS entry_anchor
		%s
		LEFT JOIN conversation_delegations cd ON cd.entry_id = ? AND cd.entry_type = ?
		LIMIT 1
	`,
		contactRefText(q.EntryType),
		q.AccountIDField,
		q.ContainerIDField,
		q.ContainerNameField,
		q.AutomationFields,
		q.AutomationColumn,
		q.statusColumnOrEmpty(),
		q.closeFieldsSQL(),
		q.messageClockFieldsSQL(),
		q.entryJoinOn("?::uuid"),
	)
}

func (q channelQuery) messageClockFieldsSQL() string {
	if q.MessageClockTable == "" {
		return "NULL::timestamptz AS last_customer_message_at, NULL::timestamptz AS last_agent_message_at, NULL::timestamptz AS awaiting_reply_since"
	}
	t := q.MessageClockTable
	return t + ".last_customer_message_at AS last_customer_message_at, " +
		t + ".last_agent_message_at AS last_agent_message_at, " +
		t + "." + sqlexpr.AwaitingReplySinceColumn + " AS awaiting_reply_since"
}

func (q channelQuery) closeFieldsSQL() string {
	if q.CloseTable == "" {
		return "''::text AS close_source, ''::text AS close_reason, ''::text AS close_outcome, NULL::timestamptz AS closed_at"
	}
	t := q.CloseTable
	return "COALESCE(" + t + ".close_source, '') AS close_source, " +
		"COALESCE(" + t + ".close_reason, '') AS close_reason, " +
		"COALESCE(" + t + ".close_outcome, '') AS close_outcome, " +
		t + ".closed_at AS closed_at"
}

func (q channelQuery) statusColumnOrEmpty() string {
	if q.StatusColumn == "" {
		return "''::text"
	}
	return q.StatusColumn
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
