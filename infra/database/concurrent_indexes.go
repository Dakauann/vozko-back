package database

import (
	"errors"
	"fmt"
	"log"

	"gorm.io/gorm"

	"vozko/infra/database/schema"
)

var errReplacementNotValid = errors.New("indexes: the replacement index is not valid yet, the index it replaces is kept")

type concurrentIndex struct {
	name     string
	sql      string
	replaces string
}

func concurrentIndexes() []concurrentIndex {
	return append([]concurrentIndex{
		{
			name: "idx_conv_event_ws_type_created",
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_conv_event_ws_type_created
				ON conversation_events (workspace_id, event_type, created_at)`,
		},
		{
			name: "idx_ca_waiting",
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ca_waiting
				ON audience_analyses (workspace_id)
				WHERE status IN ('pending', 'in_flight') AND deleted_at IS NULL`,
		},
		{
			name: "idx_cm_unread_contact",
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cm_unread_contact
				ON conversation_messages (entry_id, entry_type)
				WHERE read = false AND deleted_at IS NULL AND ` + SentByContactSQL(""),
		},
		{
			name: "idx_cm_sender_unknown",
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cm_sender_unknown
				ON conversation_messages (id)
				WHERE sender_kind = 'unknown'`,
		},
		{
			name: schema.CustomFieldLiveKeyIndex,
			sql: `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS ` + schema.CustomFieldLiveKeyIndex + `
				ON custom_field_definitions (workspace_id, object_type, key)
				WHERE deleted_at IS NULL`,
			replaces: "idx_custom_field_ws_object_key",
		},
		{
			name: schema.CustomFieldLiveRoleIndex,
			sql: `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS ` + schema.CustomFieldLiveRoleIndex + `
				ON custom_field_definitions (workspace_id, object_type, role)
				WHERE role IS NOT NULL AND role <> '' AND deleted_at IS NULL`,
		},
		{
			name: LeadCustomFieldsIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + LeadCustomFieldsIndex + `
				ON leads USING gin (custom_fields jsonb_path_ops)
				WHERE custom_fields IS NOT NULL`,
		},
		{
			name: "ux_leads_workspace_id_id",
			sql: `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS ux_leads_workspace_id_id
				ON leads (workspace_id, id)`,
		},
		{
			name: LeadLiveIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + LeadLiveIndex + `
				ON leads (workspace_id, id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_leads_workspace_owner",
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_leads_workspace_owner
				ON leads (workspace_id, owner_id)
				WHERE owner_id IS NOT NULL AND deleted_at IS NULL`,
		},
		{
			name: "idx_leads_workspace_birthday",
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_leads_workspace_birthday
				ON leads (workspace_id, (extract(month from birth_date)), (extract(day from birth_date)))
				WHERE birth_date IS NOT NULL AND deleted_at IS NULL`,
		},
		{
			name: "idx_leads_workspace_referred",
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_leads_workspace_referred
				ON leads (workspace_id, referred_count)
				WHERE referred_count > 0`,
		},
		{
			name: "idx_lead_addresses_workspace_place",
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_lead_addresses_workspace_place
				ON lead_addresses (workspace_id, city_key, district_key)
				WHERE is_primary`,
		},
		{
			name: GeocodeQueueIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + GeocodeQueueIndex + `
				ON lead_addresses (workspace_id, geo_next_at) INCLUDE (geo_status)
				WHERE ` + GeocodeQueuedSQL(""),
			replaces: "idx_lead_addresses_geo_queue",
		},
		{
			name: LeadAddressWindowIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + LeadAddressWindowIndex + `
				ON lead_addresses (workspace_id, latitude, longitude) INCLUDE (lead_id, geo_precision)
				WHERE latitude IS NOT NULL AND is_primary`,
		},
		{
			name: LeadAddressLocatedIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + LeadAddressLocatedIndex + `
				ON lead_addresses (id)
				WHERE latitude IS NOT NULL`,
		},
		{
			name: "idx_lead_addresses_import",
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_lead_addresses_import
				ON lead_addresses (import_id) INCLUDE (workspace_id, lead_id, latitude, geo_status, geo_precision)
				WHERE import_id IS NOT NULL`,
		},
		{
			name: "idx_opportunities_workspace_lead",
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_opportunities_workspace_lead
				ON opportunities (workspace_id, lead_id, created_at)
				WHERE lead_id IS NOT NULL AND deleted_at IS NULL`,
		},
		{
			name: UnlinkedCallsByCounterpartIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + UnlinkedCallsByCounterpartIndex + `
				ON calls (workspace_id, ` + CallCounterpartSQL("") + `, started_at DESC)
				WHERE lead_id IS NULL AND deleted_at IS NULL`,
			replaces: "idx_calls_workspace_counterpart",
		},
		{
			name: CallListAgendaIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + CallListAgendaIndex + `
				ON call_list_items (list_id, (` + CallListAgendaSQL("") + `), position)
				WHERE state = 'pending'`,
		},
		{
			name: CampaignEntryLiveStatusIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + CampaignEntryLiveStatusIndex + `
				ON whatsapp_campaign_entries (campaign_id, status) INCLUDE (lead_id)
				WHERE deleted_at IS NULL`,
			replaces: "idx_wce_campaign_status_del",
		},
		{
			name: LeadPhoneHoldersIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + LeadPhoneHoldersIndex + `
				ON lead_phones (workspace_id, number, lead_id)`,
			replaces: "idx_lead_phones_workspace_number",
		},
	}, append(placeSearchIndexes(), leadSearchIndexes()...)...)
}

const LeadPhoneHoldersIndex = "idx_lead_phones_workspace_number_lead"

const (
	LeadAddressWindowIndex          = "idx_lead_addresses_window"
	LeadAddressLocatedIndex         = "idx_lead_addresses_located_id"
	LeadLiveIndex                   = "idx_leads_workspace_live"
	UnlinkedCallsByCounterpartIndex = "idx_calls_ws_counterpart_unlinked"
	LeadCustomFieldsIndex           = "idx_leads_custom_fields"
	CampaignEntryLiveStatusIndex    = "idx_wce_campaign_status_live"
)

func ConcurrentIndexSQL(name string) (string, bool) {
	for _, idx := range concurrentIndexes() {
		if idx.name == name {
			return idx.sql, true
		}
	}
	return "", false
}

func createConcurrentIndexes(db *gorm.DB) {
	for _, idx := range concurrentIndexes() {
		if err := createIndexConcurrently(db, idx); err != nil {
			log.Printf("[indexes] Warning: failed to create %s: %v", idx.name, err)
		}
	}
}

func createIndexConcurrently(db *gorm.DB, idx concurrentIndex) error {
	var invalid bool
	err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM pg_class c
			JOIN pg_index i ON i.indexrelid = c.oid
			WHERE c.relname = ? AND NOT i.indisvalid
		)
	`, idx.name).Scan(&invalid).Error
	if err != nil {
		return err
	}
	if invalid {
		if err := db.Exec("DROP INDEX CONCURRENTLY IF EXISTS " + idx.name).Error; err != nil {
			return err
		}
	}
	if err := db.Exec(idx.sql).Error; err != nil {
		return err
	}
	if idx.replaces == "" {
		return nil
	}
	return dropReplacedIndex(db, idx)
}

func dropReplacedIndex(db *gorm.DB, idx concurrentIndex) error {
	var valid bool
	err := db.Raw(`SELECT COALESCE((SELECT i.indisvalid FROM pg_index i WHERE i.indexrelid = to_regclass(?::text)), false)`, idx.name).
		Scan(&valid).Error
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("%w: %s replacing %s", errReplacementNotValid, idx.name, idx.replaces)
	}
	return db.Exec("DROP INDEX CONCURRENTLY IF EXISTS " + idx.replaces).Error
}
