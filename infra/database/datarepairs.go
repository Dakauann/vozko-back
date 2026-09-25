package database

import (
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type dataRepair struct {
	name string
	run  func(*gorm.DB) error
}

func repairNames() []string {
	all := dataRepairs()
	names := make([]string, 0, len(all))
	for _, r := range all {
		names = append(names, r.name)
	}
	return names
}

func dataRepairs() []dataRepair {
	return []dataRepair{
		{"uw_clear_group_contact_phone_numbers", clearGroupContactPhoneNumbers},
		{"uw_backfill_conversation_campaigns", backfillConversationCampaigns},
		{"uw_merge_split_group_conversations", mergeSplitGroupConversations},
		{"uw_retire_unattributable_conversations", retireUnattributableConversations},
		{"uw_reset_never_read_profile_clocks", resetNeverReadProfileClocks},
		{"cm_backfill_message_direction", backfillMessageDirection},
		{"cm_correct_uw_device_sent_direction", correctUnofficialDeviceSentDirection},
		{"cm_relink_uw_orphaned_media", relinkUnofficialOrphanedMedia},
		{"ig_repair_second_timestamps", repairInstagramSecondTimestamps},
		{"wmp_drop_retired_resources", dropRetiredPermissionResources},
		{"cs_rename_dialer_resource_permissions", renameDialerResourcePermissions},
		{"cs_rename_dialer_presence_source", renameDialerPresenceSource},
		{"stg_materialize_stage_group_pipelines", materializeStageGroupPipelines},
		{"pl_demote_duplicate_default_pipelines", demoteDuplicateDefaultPipelines},
		{"opp_close_valued_deals_on_won_stages", closeValuedDealsOnWonStages},
		{"rag_size_documents_and_bases", sizeKnowledgeBaseDocuments},
	}
}

func repairInstagramSecondTimestamps(tx *gorm.DB) error {
	if err := tx.Exec(`
		UPDATE instagram_comments
		   SET timestamp = created_at
		 WHERE timestamp IS NOT NULL
		   AND timestamp < TIMESTAMPTZ '2000-01-01'
	`).Error; err != nil {
		return err
	}
	if err := tx.Exec(`
		UPDATE audience_analyses
		   SET occurred_at = created_at
		 WHERE occurred_at < TIMESTAMPTZ '2000-01-01'
		   AND created_at >= TIMESTAMPTZ '2000-01-01'
		   AND source = 'instagram'
	`).Error; err != nil {
		return err
	}
	return nil
}

func runDataRepairs(tx *gorm.DB) error {
	for _, r := range dataRepairs() {
		if err := r.run(tx); err != nil {
			return fmt.Errorf("data repair %s: %w", r.name, err)
		}
	}
	return nil
}

func clearGroupContactPhoneNumbers(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("unofficial_whatsapp_contacts") ||
		!tx.Migrator().HasColumn("unofficial_whatsapp_contacts", "is_group") {
		return nil
	}
	return tx.Exec(`
		UPDATE unofficial_whatsapp_contacts
		SET is_group = true, phone_number = '', lead_id = NULL
		WHERE jid LIKE '%@g.us'
		  AND (is_group = false OR phone_number <> '' OR lead_id IS NOT NULL)
	`).Error
}

// backfillConversationCampaigns keys each existing conversation with the
// campaign the inbox already attributes it to: the latest entry sent into it.
// Only unkeyed rows change, so it is a no-op after the first boot.
func backfillConversationCampaigns(tx *gorm.DB) error {
	return tx.Exec(`
		UPDATE unofficial_whatsapp_conversations AS c
		SET campaign_id = latest.campaign_id::text
		FROM (
			SELECT DISTINCT ON (e.conversation_id) e.conversation_id, e.campaign_id
			FROM unofficial_whatsapp_campaign_entries e
			WHERE e.conversation_id IS NOT NULL AND e.deleted_at IS NULL
			ORDER BY e.conversation_id, e.sent_at DESC NULLS LAST, e.updated_at DESC
		) AS latest
		WHERE latest.conversation_id = c.id
		  AND c.campaign_id = ''
	`).Error
}

func retireUnattributableConversations(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("unofficial_whatsapp_conversations") ||
		!tx.Migrator().HasTable("unofficial_whatsapp_contacts") {
		return nil
	}
	return tx.Exec(`
		UPDATE unofficial_whatsapp_conversations AS c
		SET deleted_at = NOW()
		FROM unofficial_whatsapp_contacts AS s
		WHERE s.id = c.contact_id
		  AND c.deleted_at IS NULL
		  AND COALESCE(c.chat_id, '') = ''
		  AND COALESCE(s.jid, '') = ''
	`).Error
}

func resetNeverReadProfileClocks(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("unofficial_whatsapp_contacts") {
		return nil
	}
	return tx.Exec(`
		UPDATE unofficial_whatsapp_contacts
		SET profile_fetched_at = NULL
		WHERE profile_fetched_at IS NOT NULL
		  AND COALESCE(picture_url, '') = ''
		  AND deleted_at IS NULL
	`).Error
}

type entryTable struct {
	name        string
	idIsText    bool
	dedupeOn    []string
	softDeleted bool
}

func (t entryTable) idExpr(column string) string {
	if t.idIsText {
		return column + "::text"
	}
	return column
}

// uwDuplicateConversationsSQL pairs each duplicate conversation with the one
// it merges into. A chat holds one conversation per campaign (and one without),
// as an official number holds one entry per campaign, so the campaign is part
// of the key: only true duplicates of the same chat and campaign merge.
const uwDuplicateConversationsSQL = `
		SELECT id AS duplicate_id, survivor_id
		FROM (
			SELECT id,
			       FIRST_VALUE(id) OVER (
			           PARTITION BY instance_id, chat_id, campaign_id
			           ORDER BY created_at ASC, id ASC
			       ) AS survivor_id
			FROM unofficial_whatsapp_conversations
			WHERE deleted_at IS NULL
		) ranked
		WHERE id <> survivor_id
	`

func mergeSplitGroupConversations(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("unofficial_whatsapp_conversations") ||
		!tx.Migrator().HasColumn("unofficial_whatsapp_conversations", "chat_id") ||
		!tx.Migrator().HasColumn("unofficial_whatsapp_conversations", "campaign_id") {
		return nil
	}

	const duplicatesCTE = uwDuplicateConversationsSQL

	var duplicateCount int64
	if err := tx.Raw(`SELECT COUNT(*) FROM (` + duplicatesCTE + `) d`).
		Scan(&duplicateCount).Error; err != nil {
		return err
	}
	if duplicateCount == 0 {
		return repointGroupSubjects(tx)
	}

	appendOnly := []entryTable{
		{name: "conversation_messages"},
		{name: "conversation_media"},
		{name: "conversation_events"},
		{name: "analyses"},
		{name: "assignment_history"},
		{name: "workflow_runs"},
		{name: "ai_attendance_sessions", idIsText: true},
	}
	for _, t := range appendOnly {
		if !tx.Migrator().HasTable(t.name) {
			continue
		}
		sql := fmt.Sprintf(`
			UPDATE %s AS t
			SET entry_id = %s
			FROM (%s) AS d
			WHERE %s = %s
			  AND t.entry_type = 'unofficial_whatsapp'
		`, t.name, t.idExpr("d.survivor_id"), duplicatesCTE,
			"t.entry_id", t.idExpr("d.duplicate_id"))
		if err := tx.Exec(sql).Error; err != nil {
			return fmt.Errorf("repointing %s: %w", t.name, err)
		}
	}

	guarded := []entryTable{
		{name: "inbox_assignments"},
		{name: "entry_stages", dedupeOn: []string{"stage_id"}, softDeleted: true},
		{name: "entry_labels", dedupeOn: []string{"label_id"}, softDeleted: true},
		{name: "opportunity_conversations", dedupeOn: []string{"opportunity_id"}},
	}
	for _, t := range guarded {
		if !tx.Migrator().HasTable(t.name) {
			continue
		}

		match := "existing.entry_id = d.survivor_id AND existing.entry_type = 'unofficial_whatsapp'"
		for _, col := range t.dedupeOn {
			match += fmt.Sprintf(" AND existing.%s = t.%s", col, col)
		}
		if t.softDeleted {
			match += " AND existing.deleted_at IS NULL"
		}

		move := fmt.Sprintf(`
			UPDATE %[1]s AS t
			SET entry_id = d.survivor_id
			FROM (%[2]s) AS d
			WHERE t.entry_id = d.duplicate_id
			  AND t.entry_type = 'unofficial_whatsapp'
			  AND NOT EXISTS (SELECT 1 FROM %[1]s AS existing WHERE %[3]s)
		`, t.name, duplicatesCTE, match)
		if err := tx.Exec(move).Error; err != nil {
			return fmt.Errorf("repointing %s: %w", t.name, err)
		}

		drop := fmt.Sprintf(`
			DELETE FROM %s AS t
			USING (%s) AS d
			WHERE t.entry_id = d.duplicate_id
			  AND t.entry_type = 'unofficial_whatsapp'
		`, t.name, duplicatesCTE)
		if err := tx.Exec(drop).Error; err != nil {
			return fmt.Errorf("clearing leftover %s: %w", t.name, err)
		}
	}

	soft := fmt.Sprintf(`
		UPDATE unofficial_whatsapp_conversations AS c
		SET deleted_at = NOW()
		FROM (%s) AS d
		WHERE c.id = d.duplicate_id
	`, duplicatesCTE)
	if err := tx.Exec(soft).Error; err != nil {
		return fmt.Errorf("soft-deleting merged conversations: %w", err)
	}

	return repointGroupSubjects(tx)
}

func repointGroupSubjects(tx *gorm.DB) error {
	if !tx.Migrator().HasColumn("unofficial_whatsapp_contacts", "is_group") {
		return nil
	}
	return tx.Exec(`
		UPDATE unofficial_whatsapp_conversations AS c
		SET contact_id = g.id
		FROM unofficial_whatsapp_contacts AS g
		WHERE c.is_group = true
		  AND c.deleted_at IS NULL
		  AND g.instance_id = c.instance_id
		  AND g.jid = c.chat_id
		  AND g.deleted_at IS NULL
		  AND c.contact_id <> g.id
	`).Error
}

func backfillMessageDirection(tx *gorm.DB) error {
	return nil
}

func relinkUnofficialOrphanedMedia(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("conversation_media") ||
		!tx.Migrator().HasTable("conversation_messages") {
		return nil
	}
	return tx.Exec(`
		UPDATE conversation_messages AS m
		SET media_id   = src.media_id,
		    media_type = src.media_type,
		    text       = CASE
		                   WHEN m.text IN ('[imagem]','[vídeo]','[áudio]','[documento]','[figurinha]')
		                   THEN ''
		                   ELSE m.text
		                 END
		FROM (
			SELECT cm.id      AS media_id,
			       cm.type    AS media_type,
			       cm.entry_id,
			       regexp_replace(
			           regexp_replace(cm.url, '^.*/', ''),
			           '\.[^.]*$', ''
			       ) AS provider_message_id
			FROM conversation_media AS cm
			WHERE cm.entry_type = 'unofficial_whatsapp'
			  AND cm.deleted_at IS NULL
		) AS src
		WHERE m.entry_type = 'unofficial_whatsapp'
		  AND m.media_id IS NULL
		  AND m.deleted_at IS NULL
		  AND m.entry_id = src.entry_id
		  AND m.external_message_id = src.provider_message_id
	`).Error
}

func correctUnofficialDeviceSentDirection(tx *gorm.DB) error {
	return tx.Exec(`
		UPDATE conversation_messages AS m
		SET direction = 'OUTBOUND'
		WHERE m.entry_type = 'unofficial_whatsapp'
		  AND m.direction = 'INBOUND'
		  AND m.message_type IN ('user_message','audio','media')
		  AND COALESCE(m.from_participant, '') <> ''
		  AND EXISTS (
			SELECT 1
			FROM conversation_messages AS ours
			WHERE ours.entry_id = m.entry_id
			  AND ours.entry_type = m.entry_type
			  AND ours.message_type IN ('operator','ai_response','template')
			  AND ours.from_participant = m.from_participant
		  )
	`).Error
}

var retiredPermissionResources = []string{
	"sip_trunks",
	"branches",
	"campaigns",
	"affiliate",
	"usage",
}

func dropRetiredPermissionResources(tx *gorm.DB) error {
	result := tx.Exec(
		"DELETE FROM workspace_member_permissions WHERE resource IN ?",
		retiredPermissionResources,
	)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		log.Printf("[data-repair] removed %d permission grant(s) for retired resources %v",
			result.RowsAffected, retiredPermissionResources)
	}
	return nil
}

const (
	legacyCallSessionValue    = "dialer"
	callSessionResourceValue  = "call_session"
	callSessionPresenceSource = "call_session"
)

func renameDialerResourcePermissions(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("workspace_member_permissions") {
		return nil
	}

	var stale int64
	if err := tx.Raw(
		"SELECT COUNT(*) FROM workspace_member_permissions WHERE resource = ?",
		legacyCallSessionValue,
	).Scan(&stale).Error; err != nil {
		return err
	}
	if stale == 0 {
		return nil
	}

	if err := tx.Exec(`
		DELETE FROM workspace_member_permissions AS legacy
		WHERE legacy.resource = ?
		  AND EXISTS (
			SELECT 1
			FROM workspace_member_permissions AS existing
			WHERE existing.member_id = legacy.member_id
			  AND existing.action    = legacy.action
			  AND existing.resource  = ?
		  )
	`, legacyCallSessionValue, callSessionResourceValue).Error; err != nil {
		return err
	}

	result := tx.Exec(
		"UPDATE workspace_member_permissions SET resource = ? WHERE resource = ?",
		callSessionResourceValue, legacyCallSessionValue,
	)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		log.Printf("[data-repair] renamed %d permission grant(s) from %q to %q",
			result.RowsAffected, legacyCallSessionValue, callSessionResourceValue)
	}
	return nil
}

func renameDialerPresenceSource(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("agent_presence_intervals") {
		return nil
	}

	var stale int64
	if err := tx.Raw(
		"SELECT COUNT(*) FROM agent_presence_intervals WHERE source = ?",
		legacyCallSessionValue,
	).Scan(&stale).Error; err != nil {
		return err
	}
	if stale == 0 {
		return nil
	}

	result := tx.Exec(
		"UPDATE agent_presence_intervals SET source = ? WHERE source = ?",
		callSessionPresenceSource, legacyCallSessionValue,
	)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		log.Printf("[data-repair] renamed %d presence interval(s) from source %q to %q",
			result.RowsAffected, legacyCallSessionValue, callSessionPresenceSource)
	}
	return nil
}

func materializeStageGroupPipelines(tx *gorm.DB) error {
	type orphanGroup struct {
		ID          string
		WorkspaceID string
		Name        string
	}

	var orphans []orphanGroup
	err := tx.Raw(`
		SELECT g.id, g.workspace_id, g.name
		FROM stage_groups g
		WHERE g.deleted_at IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM pipelines p
		      WHERE p.stage_group_id::text = g.id
		        AND p.workspace_id = g.workspace_id
		        AND p.deleted_at IS NULL
		  )
	`).Scan(&orphans).Error
	if err != nil {
		log.Printf("[data-repair] stage-group funnels: skipped (%v)", err)
		return nil
	}
	if len(orphans) == 0 {
		return nil
	}

	type groupItem struct {
		Name        string
		Description string
		Color       string
		Position    int
	}

	created := 0
	for _, g := range orphans {
		name := strings.TrimSpace(g.Name)
		if name == "" {
			name = "Funil"
		}

		var items []groupItem
		if err := tx.Raw(`
			SELECT name, description, color, position
			FROM stage_group_items
			WHERE stage_group_id = ?
			ORDER BY position ASC
		`, g.ID).Scan(&items).Error; err != nil {
			return fmt.Errorf("read items of stage group %s: %w", g.ID, err)
		}
		if len(items) == 0 {
			continue
		}

		var maxPos int
		if err := tx.Raw(`
			SELECT COALESCE(MAX(position), 0) FROM pipelines
			WHERE workspace_id = ? AND object_type = 'conversation' AND deleted_at IS NULL
		`, g.WorkspaceID).Scan(&maxPos).Error; err != nil {
			return fmt.Errorf("read funnel positions for workspace %s: %w", g.WorkspaceID, err)
		}

		pipelineID := uuid.New().String()
		if err := tx.Exec(`
			INSERT INTO pipelines (id, workspace_id, name, object_type, stage_group_id, position, is_default, created_at, updated_at)
			VALUES (?, ?, ?, 'conversation', ?, ?, false, NOW(), NOW())
		`, pipelineID, g.WorkspaceID, name, g.ID, maxPos+1).Error; err != nil {
			return fmt.Errorf("create funnel for stage group %s: %w", g.ID, err)
		}

		for i, it := range items {
			if err := tx.Exec(`
				INSERT INTO stages (id, workspace_id, pipeline_id, name, description, color, position, is_initial, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())
			`,
				uuid.New().String(), g.WorkspaceID, pipelineID,
				strings.ToLower(strings.TrimSpace(it.Name)), it.Description, it.Color,
				it.Position, i == 0,
			).Error; err != nil {
				return fmt.Errorf("clone stage %q of group %s: %w", it.Name, g.ID, err)
			}
		}
		created++
	}

	if created > 0 {
		log.Printf("[data-repair] stage-group funnels: materialized %d funnel(s) for groups that had none", created)
	}
	return nil
}

func demoteDuplicateDefaultPipelines(tx *gorm.DB) error {
	res := tx.Exec(`
		WITH scored AS (
			SELECT p.id, p.workspace_id, p.object_type, p.created_at,
			       (SELECT COUNT(*)
			          FROM entry_stages es
			          JOIN stages s ON s.id = es.stage_id
			         WHERE s.pipeline_id = p.id
			           AND es.deleted_at IS NULL) AS staged
			FROM pipelines p
			WHERE p.deleted_at IS NULL AND p.is_default = true
		),
		ranked AS (
			SELECT id,
			       ROW_NUMBER() OVER (
			           PARTITION BY workspace_id, object_type
			           ORDER BY staged DESC, created_at ASC, id ASC
			       ) AS rn
			FROM scored
		)
		UPDATE pipelines
		   SET is_default = false
		 WHERE is_default = true
		   AND id IN (SELECT id FROM ranked WHERE rn > 1)
	`)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		log.Printf("[data-repair] demoted %d duplicate default funnel(s)", res.RowsAffected)
	}
	return nil
}
