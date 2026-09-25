package database

import (
	"fmt"
	"log"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/balance"
	ia "vozko/domain/inbox_assignment"
)

func SQLStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func SQLStringLiteralList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = SQLStringLiteral(v)
	}
	return strings.Join(quoted, ", ")
}

func CreatePerformanceIndexes(db *gorm.DB) {
	indexes := []struct {
		name string
		sql  string
	}{

		{
			name: "idx_cm_inbox_whatsapp",
			sql: `CREATE INDEX IF NOT EXISTS idx_cm_inbox_whatsapp
				ON conversation_messages (entry_id, created_at DESC)
				WHERE entry_type = 'whatsapp' AND deleted_at IS NULL`,
		},
		{
			name: "idx_cm_unread_count (superseded by idx_cm_unread_contact)",
			sql:  `DROP INDEX IF EXISTS idx_cm_unread_count`,
		},
		{
			name: "idx_cm_entry_del_created",
			sql: `CREATE INDEX IF NOT EXISTS idx_cm_entry_del_created
				ON conversation_messages (entry_id, entry_type, created_at DESC)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_cm_wamid_partial",
			sql: `CREATE INDEX IF NOT EXISTS idx_cm_wamid_partial
				ON conversation_messages (whatsapp_message_id)
				WHERE whatsapp_message_id IS NOT NULL AND whatsapp_message_id != '' AND deleted_at IS NULL`,
		},
		{
			name: "idx_cm_service_exposure (superseded by idx_cm_meta_service_cost)",
			sql:  `DROP INDEX IF EXISTS idx_cm_service_exposure`,
		},
		{
			name: "idx_cm_meta_service_cost",
			sql: fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_cm_meta_service_cost
				ON conversation_messages (created_at)
				INCLUDE (entry_id, delivery_status)
				WHERE %s`, ServiceMessageIndexPredicateSQL("")),
		},

		{
			name: "idx_inbox_assignments_entry_id",
			sql: `CREATE INDEX IF NOT EXISTS idx_inbox_assignments_entry_id
				ON inbox_assignments (entry_id)`,
		},
		{
			name: "idx_inbox_assignments_entry_user",
			sql: `CREATE INDEX IF NOT EXISTS idx_inbox_assignments_entry_user
				ON inbox_assignments (entry_id, assigned_user_id)`,
		},

		{
			name: "idx_wmp_member_resource_action",
			sql: `CREATE INDEX IF NOT EXISTS idx_wmp_member_resource_action
				ON workspace_member_permissions (member_id, resource, action)`,
		},

		{
			name: "idx_wce_campaign_del",
			sql: `CREATE INDEX IF NOT EXISTS idx_wce_campaign_del
				ON whatsapp_campaign_entries (campaign_id, deleted_at)`,
		},
		{
			name: "idx_wce_campaign_status_created",
			sql: `CREATE INDEX IF NOT EXISTS idx_wce_campaign_status_created
				ON whatsapp_campaign_entries (campaign_id, status, created_at)`,
		},
		{
			name: "idx_wce_message_id",
			sql: `CREATE INDEX IF NOT EXISTS idx_wce_message_id
				ON whatsapp_campaign_entries (message_id)
				WHERE message_id IS NOT NULL AND message_id != ''`,
		},
		{
			name: "idx_wce_campaign_updated",
			sql: `CREATE INDEX IF NOT EXISTS idx_wce_campaign_updated
				ON whatsapp_campaign_entries (campaign_id, updated_at DESC)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_wce_campaign_status_del",
			sql: `CREATE INDEX IF NOT EXISTS idx_wce_campaign_status_del
				ON whatsapp_campaign_entries (campaign_id, status)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_wce_campaign_convstatus_active",
			sql: `CREATE INDEX IF NOT EXISTS idx_wce_campaign_convstatus_active
				ON whatsapp_campaign_entries (campaign_id, conversation_status)
				WHERE deleted_at IS NULL AND conversation_status <> ''`,
		},

		{
			name: "idx_analysis_entry_type_created",
			sql: `CREATE INDEX IF NOT EXISTS idx_analysis_entry_type_created
				ON analyses (entry_id, entry_type, created_at DESC)`,
		},
		{
			name: "idx_analysis_entry_composite",
			sql: `CREATE INDEX IF NOT EXISTS idx_analysis_entry_composite
				ON analyses (entry_id, entry_type)`,
		},

		{
			name: "idx_balances_workspace_id",
			sql: `CREATE INDEX IF NOT EXISTS idx_balances_workspace_id
				ON balances (workspace_id)`,
		},

		{
			name: "idx_leads_number",
			sql: `CREATE INDEX IF NOT EXISTS idx_leads_number
				ON leads (number)`,
		},
		{
			name: "idx_leads_workspace_created",
			sql: `CREATE INDEX IF NOT EXISTS idx_leads_workspace_created
				ON leads (workspace_id, created_at)
				WHERE deleted_at IS NULL`,
		},

		{
			name: "idx_wa_campaign_ws_del",
			sql: `CREATE INDEX IF NOT EXISTS idx_wa_campaign_ws_del
				ON whatsapp_campaigns (workspace_id, deleted_at)`,
		},
		{
			name: "idx_wa_campaign_ws_created",
			sql: `CREATE INDEX IF NOT EXISTS idx_wa_campaign_ws_created
				ON whatsapp_campaigns (workspace_id, created_at)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_wce_campaign_created_active",
			sql: `CREATE INDEX IF NOT EXISTS idx_wce_campaign_created_active
				ON whatsapp_campaign_entries (campaign_id, created_at)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_cm_type_created_active",
			sql: `CREATE INDEX IF NOT EXISTS idx_cm_type_created_active
				ON conversation_messages (entry_type, created_at)
				INCLUDE (entry_id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_ah_ws_started",
			sql: `CREATE INDEX IF NOT EXISTS idx_ah_ws_started
				ON assignment_history (workspace_id, started_at DESC)`,
		},
		{
			name: "idx_ah_rescue_scan (superseded by idx_ah_rescue_candidates)",
			sql:  `DROP INDEX IF EXISTS idx_ah_rescue_scan`,
		},
		{
			name: "idx_ah_rescue_candidates",
			sql: fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_ah_rescue_candidates
				ON assignment_history (workspace_id, started_at)
				WHERE ended_at IS NULL AND "trigger" IN (%s)`,
				SQLStringLiteralList(ia.RescueCandidateTriggers)),
		},
		{
			name: "idx_ia_ws_created",
			sql: `CREATE INDEX IF NOT EXISTS idx_ia_ws_created
				ON inbox_assignments (workspace_id, created_at)
				WHERE assigned_user_id IS NOT NULL`,
		},
		{
			name: "idx_ia_ws_user",
			sql: `CREATE INDEX IF NOT EXISTS idx_ia_ws_user
				ON inbox_assignments (workspace_id, assigned_user_id)`,
		},

		{
			name: "idx_entry_stages_stage_entry",
			sql: `CREATE INDEX IF NOT EXISTS idx_entry_stages_stage_entry
				ON entry_stages (stage_id, entry_type, entry_id)`,
		},
		{
			name: "idx_entry_stages_entry",
			sql: `CREATE INDEX IF NOT EXISTS idx_entry_stages_entry
				ON entry_stages (entry_id, entry_type)`,
		},

		{
			name: "idx_entry_stages_ws_del",
			sql: `CREATE INDEX IF NOT EXISTS idx_entry_stages_ws_del
				ON entry_stages (workspace_id, entry_id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_entry_stages_stage_ws_del",
			sql: `CREATE INDEX IF NOT EXISTS idx_entry_stages_stage_ws_del
				ON entry_stages (stage_id, workspace_id, entry_id)
				WHERE deleted_at IS NULL`,
		},

		{
			name: "idx_balance_tx_summary",
			sql: `CREATE INDEX IF NOT EXISTS idx_balance_tx_summary
				ON balance_transactions (balance_id, resource_type, type, amount)`,
		},

		{
			name: "idx_bt_reporting",
			sql: `CREATE INDEX IF NOT EXISTS idx_bt_reporting
				ON balance_transactions (created_at)
				INCLUDE (workspace_id, service_type, type, is_refund, amount, cost_micros, profit_micros, exchange_rate_micros)`,
		},
		{
			name: "idx_bt_campaign_created_ws",
			sql: fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_bt_campaign_created_ws
				ON balance_transactions (created_at)
				INCLUDE (workspace_id, type, is_refund)
				WHERE service_type = %s`,
				SQLStringLiteral(string(balance.ServiceWhatsAppCampaign))),
		},
		{
			name: "idx_bt_ws_svc_created_charges",
			sql: `CREATE INDEX IF NOT EXISTS idx_bt_ws_svc_created_charges
				ON balance_transactions (workspace_id, service_type, created_at)
				INCLUDE (type, is_refund, description, reference_id)
				WHERE service_type = 'whatsapp_campaign'`,
		},
		{
			name: "idx_wats_unsettled",
			sql: `CREATE INDEX IF NOT EXISTS idx_wats_unsettled
				ON whatsapp_template_sends (updated_at)
				WHERE status IN ('pending', 'charged', 'unknown') AND deleted_at IS NULL`,
		},
		{
			name: "idx_wats_wamid",
			sql: `CREATE INDEX IF NOT EXISTS idx_wats_wamid
				ON whatsapp_template_sends (workspace_id, provider_message_id)
				WHERE provider_message_id <> '' AND deleted_at IS NULL`,
		},
		{
			name: "idx_wc_ws_type_dept",
			sql: `CREATE INDEX IF NOT EXISTS idx_wc_ws_type_dept
				ON whatsapp_campaigns (workspace_id, type, department_id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_ws_sub_plan_status_ws",
			sql: `CREATE INDEX IF NOT EXISTS idx_ws_sub_plan_status_ws
				ON workspace_subscriptions (plan_definition_id, status, workspace_id)`,
		},
		{
			name: "idx_invoices_subscription_reporting",
			sql: `CREATE INDEX IF NOT EXISTS idx_invoices_subscription_reporting
				ON invoices (status, created_at)
				INCLUDE (plan_definition_id, billing_cycle, workspace_id, amount_usd, amount_brl)
				WHERE purpose = 'SUBSCRIPTION'`,
		},

		{
			name: "idx_cm_lateral_join",
			sql: `CREATE INDEX IF NOT EXISTS idx_cm_lateral_join
				ON conversation_messages (entry_id, entry_type, created_at DESC, id DESC)
				WHERE deleted_at IS NULL`,
		},

		{
			name: "idx_cm_unread_inbound (superseded by idx_cm_unread_contact)",
			sql:  `DROP INDEX IF EXISTS idx_cm_unread_inbound`,
		},

		{
			name: "idx_agents_workspace",
			sql: `CREATE INDEX IF NOT EXISTS idx_agents_workspace
				ON agents (workspace_id)
				WHERE deleted_at IS NULL`,
		},

		{
			name: "idx_workflows_ws_status",
			sql: `CREATE INDEX IF NOT EXISTS idx_workflows_ws_status
				ON workflows (workspace_id, status)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_workflows_trigger",
			sql: `CREATE INDEX IF NOT EXISTS idx_workflows_trigger
				ON workflows (workspace_id, trigger_type, status)
				WHERE deleted_at IS NULL`,
		},

		{
			name: "idx_wfr_workflow_status",
			sql: `CREATE INDEX IF NOT EXISTS idx_wfr_workflow_status
				ON workflow_runs (workflow_id, status)`,
		},
		{
			name: "idx_wfr_entry_active",
			sql: `CREATE INDEX IF NOT EXISTS idx_wfr_entry_active
				ON workflow_runs (workflow_id, entry_id, status)
				WHERE status IN ('running', 'waiting')`,
		},
		{
			name: "idx_wfr_wakeable",
			sql: `CREATE INDEX IF NOT EXISTS idx_wfr_wakeable
				ON workflow_runs (wake_at, status)
				WHERE status = 'waiting' AND wake_at IS NOT NULL`,
		},
		{
			name: "idx_wfr_stuck",
			sql: `CREATE INDEX IF NOT EXISTS idx_wfr_stuck
				ON workflow_runs (updated_at, status)
				WHERE status IN ('running', 'waiting')`,
		},
		{
			name: "idx_wfr_ws_active",
			sql: `CREATE INDEX IF NOT EXISTS idx_wfr_ws_active
				ON workflow_runs (workspace_id, status)
				WHERE status IN ('running', 'waiting')`,
		},
		{
			name: "idx_wfr_entry_active_only",
			sql: `CREATE INDEX IF NOT EXISTS idx_wfr_entry_active_only
				ON workflow_runs (entry_id, updated_at DESC)
				WHERE status IN ('running', 'waiting')`,
		},
		{
			name: "idx_wfr_entry_trigger_active",
			sql: `CREATE INDEX IF NOT EXISTS idx_wfr_entry_trigger_active
				ON workflow_runs (workflow_id, entry_id, trigger_node_id)
				WHERE status IN ('running', 'waiting')`,
		},

		{
			name: "idx_wfrl_run_executed",
			sql: `CREATE INDEX IF NOT EXISTS idx_wfrl_run_executed
				ON workflow_run_logs (run_id, executed_at)`,
		},

		{
			name: "idx_wce_campaign_lastmsg",
			sql: `CREATE INDEX IF NOT EXISTS idx_wce_campaign_lastmsg
				ON whatsapp_campaign_entries (campaign_id, last_message_at DESC)
				WHERE deleted_at IS NULL AND last_message_at IS NOT NULL`,
		},

		{
			name: "idx_wce_autoclose_agent",
			sql: `CREATE INDEX IF NOT EXISTS idx_wce_autoclose_agent
				ON whatsapp_campaign_entries (last_agent_message_at ASC)
				WHERE deleted_at IS NULL
				  AND conversation_status IN ('new', 'ongoing')
				  AND last_agent_message_at IS NOT NULL`,
		},
		{
			name: "idx_wsc_autoclose_enabled",
			sql: `CREATE INDEX IF NOT EXISTS idx_wsc_autoclose_enabled
				ON workspace_configs (workspace_id)
				WHERE auto_close_enabled = TRUE`,
		},
		{
			name: "idx_wce_maxage_lastmsg",
			sql: `CREATE INDEX IF NOT EXISTS idx_wce_maxage_lastmsg
				ON whatsapp_campaign_entries (last_message_at ASC)
				WHERE deleted_at IS NULL
				  AND conversation_status IN ('new', 'ongoing')
				  AND last_message_at IS NOT NULL`,
		},
		{
			name: "idx_wsc_maxage_enabled",
			sql: `CREATE INDEX IF NOT EXISTS idx_wsc_maxage_enabled
				ON workspace_configs (workspace_id)
				WHERE auto_close_max_age_enabled = TRUE`,
		},
		{
			name: "idx_short_links_ws_created",
			sql: `CREATE INDEX IF NOT EXISTS idx_short_links_ws_created
				ON short_links (workspace_id, created_at DESC)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_short_link_clicks_link_time",
			sql: `CREATE INDEX IF NOT EXISTS idx_short_link_clicks_link_time
				ON short_link_clicks (short_link_id, occurred_at DESC)`,
		},
		{
			name: "idx_short_link_daily_lookup",
			sql: `CREATE INDEX IF NOT EXISTS idx_short_link_daily_lookup
				ON short_link_daily_stats (short_link_id, dimension, day)`,
		},

		{
			name: "idx_sched_msg_due",
			sql: `CREATE INDEX IF NOT EXISTS idx_sched_msg_due
				ON scheduled_messages (scheduled_at)
				WHERE status = 'pending' AND deleted_at IS NULL`,
		},

		{
			name: "idx_sched_msg_entry_status",
			sql: `CREATE INDEX IF NOT EXISTS idx_sched_msg_entry_status
				ON scheduled_messages (entry_id, entry_type, status)
				WHERE deleted_at IS NULL`,
		},

		{
			name: "idx_rag_chunks_embedding_hnsw",
			sql: `CREATE INDEX IF NOT EXISTS idx_rag_chunks_embedding_hnsw
				ON rag_chunks USING hnsw (embedding vector_cosine_ops)
				WITH (m = 16, ef_construction = 64)`,
		},

		{
			name: "idx_ca_pending",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_pending
				ON audience_analyses (source, account_id, container_id, created_at)
				WHERE status = 'pending' AND deleted_at IS NULL`,
		},
		{
			name: "idx_ca_inflight",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_inflight
				ON audience_analyses (updated_at)
				WHERE status = 'in_flight'`,
		},
		{
			name: "idx_ca_stats",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_stats
				ON audience_analyses (workspace_id, account_id, analyzed_at DESC)
				WHERE status = 'analyzed' AND deleted_at IS NULL`,
		},
		{
			name: "idx_ca_severity",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_severity
				ON audience_analyses (account_id, severity DESC)
				WHERE severity >= 60 AND deleted_at IS NULL`,
		},
		{
			name: "idx_ca_author",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_author
				ON audience_analyses (source, account_id, author_external_id)`,
		},
		{
			name: "idx_ca_author_period",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_author_period
				ON audience_analyses (workspace_id, account_id, occurred_at DESC, author_external_id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_ca_authors_rank",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_authors_rank
				ON audience_authors (source, account_id, is_flagged, high_sev_count DESC, max_severity DESC)`,
		},
		{
			name: "idx_ca_rollups_series",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_rollups_series
				ON audience_rollups (workspace_id, scope, scope_id, bucket_date)`,
		},
	}

	for _, idx := range indexes {
		if err := db.Exec(idx.sql).Error; err != nil {
			log.Printf("[indexes] Warning: failed to create %s: %v", idx.name, err)
		}
	}
	createConcurrentIndexes(db)
}

func createSchemaConstraints(tx *gorm.DB) error {
	constraints := []struct {
		name string
		sql  string
	}{
		{
			name: "idx_short_links_code_active",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS idx_short_links_code_active
				ON short_links (code)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_pipelines_default_per_object",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_pipelines_default_per_object
				ON pipelines (workspace_id, object_type)
				WHERE is_default AND deleted_at IS NULL`,
		},
		{
			name: "idx_workspace_subscriptions_current",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS idx_workspace_subscriptions_current
				ON workspace_subscriptions (workspace_id)
				WHERE status IN ('active', 'cancelled')`,
		},
		{
			name: "idx_addon_subscription_current",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS idx_addon_subscription_current
				ON workspace_addon_subscriptions (workspace_id, addon_definition_id)
				WHERE status = 'active'`,
		},
		{
			name: "ux_invoices_idempotency_key",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_invoices_idempotency_key
				ON invoices (idempotency_key)
				WHERE idempotency_key <> ''`,
		},
		{
			name: "ux_leads_workspace_number",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_leads_workspace_number
				ON leads (workspace_id, number)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_branches_sip_user",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_branches_sip_user
				ON branches (sip_user)`,
		},

		{
			name: "idx_lead_memories_lead",
			sql: `CREATE INDEX IF NOT EXISTS idx_lead_memories_lead
				ON lead_memories (workspace_id, lead_id, created_at DESC)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_lead_memories_lead_id",
			sql: `CREATE INDEX IF NOT EXISTS idx_lead_memories_lead_id
				ON lead_memories (lead_id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_lead_memories_dedup",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_lead_memories_dedup
				ON lead_memories (workspace_id, lead_id, content_norm)
				WHERE deleted_at IS NULL`,
		},

		{
			name: "ux_ig_contact_account_igsid",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_ig_contact_account_igsid
				ON instagram_contacts (ig_account_id, igsid)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_ig_conversation_account_contact",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_ig_conversation_account_contact
				ON instagram_conversations (ig_account_id, contact_id)
				WHERE deleted_at IS NULL`,
		},

		{
			name: "ux_tg_account_bot_user",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tg_account_bot_user
				ON telegram_accounts (bot_user_id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_tg_account_business_connection",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tg_account_business_connection
				ON telegram_accounts (business_connection_id)
				WHERE business_connection_id IS NOT NULL AND deleted_at IS NULL`,
		},
		{
			name: "ux_tg_contact_account_user",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tg_contact_account_user
				ON telegram_contacts (account_id, tg_user_id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_tg_conversation_account_contact",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tg_conversation_account_contact
				ON telegram_conversations (account_id, contact_id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_tg_file_cache_account_source",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tg_file_cache_account_source
				ON telegram_file_cache (account_id, source_key)`,
		},

		{
			name: "ux_uw_instance_jid",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_instance_jid
				ON unofficial_whatsapp_instances (jid)
				WHERE jid <> '' AND deleted_at IS NULL`,
		},
		{
			name: "ux_uw_instance_delivery_token",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_instance_delivery_token
				ON unofficial_whatsapp_instances (delivery_token_hash)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_uw_instance_server_provider_id",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_instance_server_provider_id
				ON unofficial_whatsapp_instances (server_id, provider_instance_id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_uw_contact_instance_jid",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_contact_instance_jid
				ON unofficial_whatsapp_contacts (instance_id, jid)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_uw_contact_instance_lid",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_contact_instance_lid
				ON unofficial_whatsapp_contacts (instance_id, lid)
				WHERE lid <> '' AND deleted_at IS NULL`,
		},
		// A chat holds one conversation per campaign (and one without), as an
		// official number holds one entry per campaign.
		{
			name: "ux_uw_conversation_instance_contact (superseded)",
			sql:  `DROP INDEX IF EXISTS ux_uw_conversation_instance_contact`,
		},
		{
			name: "ux_uw_conversation_instance_chat (superseded)",
			sql:  `DROP INDEX IF EXISTS ux_uw_conversation_instance_chat`,
		},
		{
			name: "ux_uw_conversation_instance_contact_campaign",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_conversation_instance_contact_campaign
				ON unofficial_whatsapp_conversations (instance_id, contact_id, campaign_id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_uw_conversation_instance_chat_campaign",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_conversation_instance_chat_campaign
				ON unofficial_whatsapp_conversations (instance_id, chat_id, campaign_id)
				WHERE chat_id <> '' AND deleted_at IS NULL`,
		},
		{
			name: "idx_uw_conversation_chat_newest",
			sql: `CREATE INDEX IF NOT EXISTS idx_uw_conversation_chat_newest
				ON unofficial_whatsapp_conversations (instance_id, chat_id, created_at DESC)
				WHERE deleted_at IS NULL`,
		},

		{
			name: "ux_uw_group_instance_jid",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_group_instance_jid
				ON unofficial_whatsapp_groups (instance_id, jid)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_uw_group_participant",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_group_participant
				ON unofficial_whatsapp_group_participants (group_id, jid)`,
		},

		{
			name: "ux_cm_entry_type_external_msgid (superseded)",
			sql:  `DROP INDEX IF EXISTS ux_cm_entry_type_external_msgid`,
		},
		{
			name: "ux_cm_entry_external_msgid",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_cm_entry_external_msgid
				ON conversation_messages (entry_type, entry_id, external_message_id)
				WHERE external_message_id IS NOT NULL AND deleted_at IS NULL`,
		},

		{
			name: "ux_sched_msg_idem",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_sched_msg_idem
				ON scheduled_messages (workspace_id, idempotency_key)
				WHERE idempotency_key IS NOT NULL AND deleted_at IS NULL`,
		},
		{
			name: "ux_wa_tpl_send_idem",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_wa_tpl_send_idem
				ON whatsapp_template_sends (workspace_id, idempotency_key)
				WHERE idempotency_key IS NOT NULL AND deleted_at IS NULL`,
		},
		{
			name: "ux_ca_subject_drop_legacy",
			sql:  `DROP INDEX IF EXISTS ux_ca_source_comment`,
		},
		{
			name: "ux_ca_revision",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_ca_revision
				ON audience_analyses (source, subject_kind, subject_id, revision)`,
		},
		{
			name: "ux_ca_subject_drop_single_revision",
			sql:  `DROP INDEX IF EXISTS ux_ca_subject`,
		},
		{
			name: "idx_ca_latest_revision",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_latest_revision ON audience_analyses
				(workspace_id, source, subject_id, occurred_at DESC, created_at DESC, id DESC)
				WHERE subject_kind = 'conversation' AND deleted_at IS NULL`,
		},
		{
			name: "idx_ca_subject_key",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_subject_key ON audience_analyses
				(workspace_id, product_interest_key, occurred_at)
				WHERE product_interest_key <> '' AND deleted_at IS NULL`,
		},
		{
			name: "ux_ca_author",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_ca_author
				ON audience_authors (source, account_id, author_external_id)`,
		},
	}

	for _, c := range constraints {
		if err := tx.Exec(c.sql).Error; err != nil {
			return fmt.Errorf("creating constraint %s: %w", c.name, err)
		}
	}
	return nil
}
