package conversation_repository

import (
	"fmt"

	"vozko/domain/shared"
)

// The entry-scoped read paths, "one campaign's inbox", "these entry ids", "this
// one entry's header", differ per channel only in table and column names, in
// exactly the same way the workspace-wide UNION in entry_sources.go does.
//
// Those fragments used to be spelled out inline in eight separate `switch
// entryType` statements inside message_repository.go. Adding a channel meant
// finding all eight, and the ones that were missed failed CLOSED: an
// `invalid entry type` error, or a filter that silently matched nothing.
//
// A channel is declared ONCE below. Both entry-scoped query paths, the window
// filter, the status filter and the single-entry hydration read from it.
//
// These are trusted, code-authored constants, never user input, and are
// interpolated into query text. Every VALUE is still passed as a bind
// parameter; channel_queries_test.go asserts both properties.

// channelQuery declares how one channel's entry rows are selected and projected.
//
// "Container" is the config-carrying parent: a whatsapp_campaign for WhatsApp,
// the account row for Instagram and Telegram. The CRM shows it in the campaign
// slot, and a campaign-scoped inbox filters on it.
type channelQuery struct {
	EntryType shared.EntryType

	// EntryTable holds the entry (== the conversation) and its denormalized
	// last_message_at.
	EntryTable string

	// EntryJoin joins the entry and its container onto the entry-id column. It
	// carries exactly one %[1]s verb, bound to the caller's alias for that
	// column ("e.entry_id" or "entries.entry_id"), so both query paths share one
	// definition instead of two copies that can drift.
	EntryJoin string
	// ContactJoin exposes the sender as l.name / l.number / l.profile_picture_url
	// / l.blocked. Channels whose contacts are leads join the leads table;
	// channels whose contacts are not leads project their own table into that
	// same shape, which is what keeps every downstream predicate channel-neutral.
	ContactJoin string

	// Projections, evaluated against the aliases EntryJoin introduces.
	ContactIDField     string
	AccountIDField     string
	ContainerIDField   string
	ContainerNameField string
	// AutomationFields yields agent_id, workflow_id, agent_responses_enabled and
	// workflow_enabled, aliased exactly so.
	AutomationFields string

	// AutomationColumn is the per-conversation automation override column. Every
	// channel stores one; only WhatsApp's was ever read, so the others reported
	// automation as enabled regardless of what an operator had set.
	AutomationColumn string

	// StatusColumn is the conversation-status column, or "" when the channel has
	// none (the filter is then skipped rather than matching nothing).
	StatusColumn string

	// ContainerCTE selects entry ids belonging to one container. One bind
	// parameter (the container id) and one %[1]s verb for the assignment-scope
	// clause.
	ContainerCTE string
	// ContainerCTEEntryCol is the entry-id column inside ContainerCTE, used to
	// build that assignment-scope clause.
	ContainerCTEEntryCol string

	// ContainerFilter narrows conversation_messages to one container. One bind
	// parameter (the container id) and one %[1]s verb for the department clause.
	ContainerFilter string
	// DepartmentColumn / DepartmentEntryCol build that department clause.
	DepartmentColumn   string
	DepartmentEntryCol string

	// WindowSubquery selects entries whose outbound window is currently open, or
	// "" when the channel has no window (the filter is then a no-op instead of
	// excluding every row).
	WindowSubquery string

	// EntryInfoSQL hydrates one entry's header: lead_id, business_phone_id,
	// campaign_id, campaign_name and the automation quartet. One bind parameter,
	// the entry id.
	EntryInfoSQL string
}

// channelQueries is the registry.
//
// To add a channel: append its declaration. No query path changes.
var channelQueries = []channelQuery{
	{
		EntryType:  shared.EntryTypeWhatsApp,
		EntryTable: "whatsapp_campaign_entries",

		EntryJoin: `JOIN whatsapp_campaign_entries wce ON wce.id = %[1]s AND wce.deleted_at IS NULL
		             JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id`,
		ContactJoin: `JOIN leads l ON l.id = wce.lead_id AND l.deleted_at IS NULL`,

		ContactIDField:     "wce.lead_id::text",
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

		// WhatsApp's window lives in its own table, keyed on (lead, business phone).
		WindowSubquery: `SELECT wce_w.id FROM whatsapp_campaign_entries wce_w
				JOIN whatsapp_campaigns wc_w ON wc_w.id = wce_w.campaign_id
				JOIN lead_message_windows lmw ON lmw.lead_id = wce_w.lead_id AND lmw.business_phone_id = wc_w.business_phone_id
				WHERE wce_w.deleted_at IS NULL AND lmw.last_message_at > NOW() - INTERVAL '24 hours'`,

		EntryInfoSQL: `
			SELECT wce.lead_id::text AS lead_id, COALESCE(wc.business_phone_id::text, '') AS business_phone_id,
			       wc.id::text AS campaign_id, wc.name AS campaign_name,
			       COALESCE(wc.agent_id::text, '') AS agent_id, COALESCE(wc.workflow_id::text, '') AS workflow_id,
			       wc.enable_agent_responses AS agent_responses_enabled, wc.enable_workflow AS workflow_enabled,
			       wce.automation_enabled AS automation_enabled
			FROM whatsapp_campaign_entries wce
			JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id
			WHERE wce.id = ?::uuid AND wce.deleted_at IS NULL
			LIMIT 1
		`,
	},
	{
		// The Instagram account plays the role whatsapp_campaigns plays for
		// WhatsApp: it is the container that carries the automation config. There
		// is no campaign, so a "campaign" filter here is an account filter.
		EntryType:  shared.EntryTypeInstagram,
		EntryTable: "instagram_conversations",

		EntryJoin: `JOIN instagram_conversations igc ON igc.id = %[1]s AND igc.deleted_at IS NULL
		             JOIN instagram_accounts iga ON iga.id = igc.ig_account_id`,
		// An Instagram contact has no phone number, so the @handle takes the
		// number slot, it is what an operator actually searches by.
		ContactJoin: `JOIN (
	SELECT id, name, username AS number, profile_picture_url, blocked, deleted_at
	FROM instagram_contacts
) l ON l.id = igc.contact_id AND l.deleted_at IS NULL`,

		ContactIDField:     "igc.contact_id::text",
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
				SELECT igc_f.id::text FROM instagram_conversations igc_f
				JOIN instagram_accounts iga_f ON iga_f.id = igc_f.ig_account_id
				WHERE igc_f.ig_account_id = ? AND igc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "iga_f.department_id",
		DepartmentEntryCol: "igc_f.id",

		// Instagram's window is anchored on the conversation row itself rather
		// than on a separate window table: last_customer_message_at is the
		// sliding 24h anchor and resets on every inbound message.
		WindowSubquery: `SELECT igc_w.id::text FROM instagram_conversations igc_w
				WHERE igc_w.deleted_at IS NULL
				  AND igc_w.last_customer_message_at IS NOT NULL
				  AND igc_w.last_customer_message_at > NOW() - INTERVAL '24 hours'`,

		EntryInfoSQL: `
			SELECT igc.contact_id::text AS lead_id, COALESCE(igc.ig_account_id::text, '') AS business_phone_id,
			       iga.id::text AS campaign_id, iga.username AS campaign_name,
			       COALESCE(iga.agent_id::text, '') AS agent_id, COALESCE(iga.workflow_id::text, '') AS workflow_id,
			       iga.enable_agent_responses AS agent_responses_enabled, iga.enable_workflow AS workflow_enabled,
			       igc.automation_enabled AS automation_enabled
			FROM instagram_conversations igc
			JOIN instagram_accounts iga ON iga.id = igc.ig_account_id
			WHERE igc.id = ?::uuid AND igc.deleted_at IS NULL
			LIMIT 1
		`,
	},
	{
		// Telegram mirrors Instagram's container shape: the bot account carries
		// the department and the automation config, and a Telegram contact is not
		// a lead.
		EntryType:  shared.EntryTypeTelegram,
		EntryTable: "telegram_conversations",

		EntryJoin: `JOIN telegram_conversations tgc ON tgc.id = %[1]s AND tgc.deleted_at IS NULL
		             JOIN telegram_accounts tga ON tga.id = tgc.account_id`,
		// A Telegram contact usually has no phone number (it is only shared by an
		// explicit consent tap), so the @username takes the number slot and falls
		// back to the shared phone when one exists.
		ContactJoin: `JOIN (
	SELECT id, COALESCE(NULLIF(TRIM(CONCAT_WS(' ', first_name, last_name)), ''), username) AS name,
	       COALESCE(NULLIF(username, ''), COALESCE(phone_number, '')) AS number,
	       photo_url AS profile_picture_url, blocked, deleted_at
	FROM telegram_contacts
) l ON l.id = tgc.contact_id AND l.deleted_at IS NULL`,

		ContactIDField:     "tgc.contact_id::text",
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
				SELECT tgc_f.id::text FROM telegram_conversations tgc_f
				JOIN telegram_accounts tga_f ON tga_f.id = tgc_f.account_id
				WHERE tgc_f.account_id = ? AND tgc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "tga_f.department_id",
		DepartmentEntryCol: "tgc_f.id",

		// In bot mode Telegram has no messaging window at all, and in business
		// mode the 24h clock is anchored on the same conversation column
		// Instagram uses. Filtering on that column is therefore correct for
		// business mode and harmlessly conservative for bot mode, which is why
		// the composer consults ChannelAdapter.WindowState, the single authority,
		// rather than this filter.
		WindowSubquery: `SELECT tgc_w.id::text FROM telegram_conversations tgc_w
				WHERE tgc_w.deleted_at IS NULL
				  AND tgc_w.last_customer_message_at IS NOT NULL
				  AND tgc_w.last_customer_message_at > NOW() - INTERVAL '24 hours'`,

		EntryInfoSQL: `
			SELECT tgc.contact_id::text AS lead_id, COALESCE(tgc.account_id::text, '') AS business_phone_id,
			       tga.id::text AS campaign_id, tga.bot_username AS campaign_name,
			       COALESCE(tga.agent_id::text, '') AS agent_id, COALESCE(tga.workflow_id::text, '') AS workflow_id,
			       tga.enable_agent_responses AS agent_responses_enabled, tga.enable_workflow AS workflow_enabled,
			       tgc.automation_enabled AS automation_enabled
			FROM telegram_conversations tgc
			JOIN telegram_accounts tga ON tga.id = tgc.account_id
			WHERE tgc.id = ?::uuid AND tgc.deleted_at IS NULL
			LIMIT 1
		`,
	},
	{
		// The instance is the container that carries the automation config, so
		// a "campaign" filter on this channel is an instance filter.
		EntryType:  shared.EntryTypeUnofficialWhatsApp,
		EntryTable: "unofficial_whatsapp_conversations",

		EntryJoin: `JOIN unofficial_whatsapp_conversations uwc ON uwc.id = %[1]s AND uwc.deleted_at IS NULL
		             JOIN unofficial_whatsapp_instances uwi ON uwi.id = uwc.instance_id`,
		// Unlike Instagram and Telegram, this channel's contact HAS a phone
		// number, so the number slot carries a real dialable number and the CRM's
		// existing search-by-number works unchanged. The display name still falls
		// back through the same preference order the entity uses, so a contact
		// seen before its profile resolved renders as a number rather than blank.
		ContactJoin: `JOIN (
	SELECT id,
	       COALESCE(NULLIF(contact_name, ''), NULLIF(verified_name, ''), NULLIF(name, ''), phone_number) AS name,
	       phone_number AS number,
	       picture_url AS profile_picture_url, blocked, deleted_at
	FROM unofficial_whatsapp_contacts
) l ON l.id = uwc.contact_id AND l.deleted_at IS NULL`,

		ContactIDField:     "uwc.contact_id::text",
		AccountIDField:     "COALESCE(uwc.instance_id::text, '')",
		ContainerIDField:   "uwi.id::text",
		ContainerNameField: "uwi.display_name",
		AutomationFields: "COALESCE(uwi.agent_id::text, '') AS agent_id, " +
			"COALESCE(uwi.workflow_id::text, '') AS workflow_id, " +
			"uwi.enable_agent_responses AS agent_responses_enabled, " +
			"uwi.enable_workflow AS workflow_enabled",

		AutomationColumn: "uwc.automation_enabled",
		StatusColumn:     "uwc.conversation_status",

		ContainerCTE:         `SELECT uwc_f.id AS entry_id FROM unofficial_whatsapp_conversations uwc_f WHERE uwc_f.instance_id = ? AND uwc_f.deleted_at IS NULL%[1]s`,
		ContainerCTEEntryCol: "uwc_f.id",

		ContainerFilter: `cm.entry_id IN (
				SELECT uwc_f.id::text FROM unofficial_whatsapp_conversations uwc_f
				JOIN unofficial_whatsapp_instances uwi_f ON uwi_f.id = uwc_f.instance_id
				WHERE uwc_f.instance_id = ? AND uwc_f.deleted_at IS NULL%[1]s
			)`,
		DepartmentColumn:   "uwi_f.department_id",
		DepartmentEntryCol: "uwc_f.id",

		// EMPTY, and that is the point of the channel: there is no messaging
		// window and no template gate, so cold outbound is always permitted by
		// the platform. An empty subquery makes the window filter a no-op instead
		// of excluding every row. What actually closes the composer — a dead
		// session, a WhatsApp restriction, a block — is per-instance and
		// per-contact state, resolved in ChannelAdapter.WindowState, which is the
		// single authority both the send path and the UI consult.
		WindowSubquery: "",

		EntryInfoSQL: `
			SELECT uwc.contact_id::text AS lead_id, COALESCE(uwc.instance_id::text, '') AS business_phone_id,
			       uwi.id::text AS campaign_id, uwi.display_name AS campaign_name,
			       COALESCE(uwi.agent_id::text, '') AS agent_id, COALESCE(uwi.workflow_id::text, '') AS workflow_id,
			       uwi.enable_agent_responses AS agent_responses_enabled, uwi.enable_workflow AS workflow_enabled,
			       uwc.automation_enabled AS automation_enabled
			FROM unofficial_whatsapp_conversations uwc
			JOIN unofficial_whatsapp_instances uwi ON uwi.id = uwc.instance_id
			WHERE uwc.id = ?::uuid AND uwc.deleted_at IS NULL
			LIMIT 1
		`,
	},
}

// entryRef is one matched row from a workspace-wide query: which entry, and on
// which channel.
type entryRef struct {
	ID   string
	Type shared.EntryType
}

// entryBatch is one channel's worth of entry ids to hydrate.
type entryBatch struct {
	EntryType shared.EntryType
	IDs       []string
}

// hydrationBatches groups matched entry ids by channel, in registry order.
//
// The workspace-wide queries return a mixed-channel id list which then has to be
// hydrated one channel at a time (each needs its own joins). That grouping was
// written as a switch listing two channels, so a third channel's rows were
// silently dropped between matching and hydration, the conversation existed,
// was counted, and then vanished from the page. Driving it from the registry
// makes that impossible.
//
// Entry types with no declaration are skipped: the workspace CTE cannot emit
// them, and hydrating them would fail anyway.
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

// channelQueryFor resolves a channel's declaration.
func channelQueryFor(entryType shared.EntryType) (channelQuery, bool) {
	for _, q := range channelQueries {
		if q.EntryType == entryType {
			return q, true
		}
	}
	return channelQuery{}, false
}

// entryJoinOn renders the entry join against the caller's entry-id column.
func (q channelQuery) entryJoinOn(entryIDColumn string) string {
	return fmt.Sprintf(q.EntryJoin, entryIDColumn)
}

// containerCTE renders the container-scoped entry CTE with an assignment clause.
func (q channelQuery) containerCTE(assignmentClause string) string {
	return fmt.Sprintf(q.ContainerCTE, assignmentClause)
}

// containerFilter renders the container-scoped message filter with a department
// clause.
func (q channelQuery) containerFilter(departmentClause string) string {
	return fmt.Sprintf(q.ContainerFilter, departmentClause)
}
