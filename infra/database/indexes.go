package database

import (
	"fmt"
	"log"
	"strings"

	"gorm.io/gorm"

	ia "vozko/domain/inbox_assignment"
)

// sqlStringLiteralList renders values as a comma-separated SQL literal list.
//
// An index predicate cannot take bind parameters, so a partial index built from
// domain constants has to inline them. The inputs are compile-time constants,
// never request data; the quote doubling is belt-and-braces so this cannot
// become an injection point if someone later feeds it something dynamic.
func sqlStringLiteralList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
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
			name: "idx_cm_unread_count",
			sql: `CREATE INDEX IF NOT EXISTS idx_cm_unread_count
				ON conversation_messages (entry_id, entry_type)
				WHERE read = false AND deleted_at IS NULL
				AND message_type IN ('user_message', 'audio', 'media')`,
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
			// CountByConversationStatus was a full Parallel Seq Scan on the large
			// whatsapp_campaign_entries table; this partial composite makes it an
			// index scan. Idempotent (IF NOT EXISTS). Note: a single-column
			// idx_wce_conv_status already exists from the schema gorm tag, but the
			// status-count query filters by campaign_id, so the composite is needed.
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
			// Serves the new-leads KPI and the leads page's "criado em" filter:
			// both count a workspace's leads over a date window, which the
			// workspace-only index made a full scan of every lead the tenant has.
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
			// Serves the campaigns-summary rollup: filter a workspace's campaigns
			// by creation date before aggregating their entries. Mirror on voice
			// below. Partial on the live rows the summary actually scans.
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
			// Activity-in-period / overview: drive FROM messages by entry_type + created_at.
			name: "idx_cm_type_created_active",
			sql: `CREATE INDEX IF NOT EXISTS idx_cm_type_created_active
				ON conversation_messages (entry_type, created_at)
				INCLUDE (entry_id)
				WHERE deleted_at IS NULL`,
		},
		// FRT: assignment_history by workspace + started_at (overview + classic stats).
		{
			name: "idx_ah_ws_started",
			sql: `CREATE INDEX IF NOT EXISTS idx_ah_ws_started
				ON assignment_history (workspace_id, started_at DESC)`,
		},
		// Roulette rescue sweep: still-open roulette hand-outs, oldest first.
		//
		// Doubly partial on purpose. The schema's idx_assign_hist_open covers
		// every open interval, which is every currently-assigned conversation on
		// the platform — the sweep would scan all of them to find the handful
		// that are stalled. Narrowing to (ended_at IS NULL AND trigger IN
		// (...)) keeps the index to conversations the roulette or a previous
		// rescue handed out and nobody has since taken over, and started_at
		// serves the ORDER BY.
		//
		// The predicate is rendered from ia.RescueCandidateTriggers rather than
		// spelled out, because it has to stay a SUPERSET of the sweep's WHERE
		// clause. An index predicate narrower than the query is worse than no
		// index at all: Postgres cannot prove it covers the requested rows, so
		// it silently declines the index and the sweep goes back to scanning
		// every open assignment on the platform — with no error anywhere to say
		// so. Deriving both from one constant is what makes that undriftable.
		//
		// idx_ah_rescue_scan is its predecessor, keyed on trigger = 'inbound_rr'
		// alone. It is dropped rather than replaced in place: CREATE INDEX IF
		// NOT EXISTS on the same name is a no-op against an existing index and
		// would keep the old, too-narrow predicate forever. A new name makes
		// both statements idempotent, so neither costs anything after the first
		// boot that runs them.
		//
		// "trigger" is quoted because TRIGGER is a reserved word in SQL.
		{
			name: "idx_ah_rescue_scan (superseded by idx_ah_rescue_candidates)",
			sql:  `DROP INDEX IF EXISTS idx_ah_rescue_scan`,
		},
		{
			name: "idx_ah_rescue_candidates",
			sql: fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_ah_rescue_candidates
				ON assignment_history (workspace_id, started_at)
				WHERE ended_at IS NULL AND "trigger" IN (%s)`,
				sqlStringLiteralList(ia.RescueCandidateTriggers)),
		},
		// Classic attendance: assignments by workspace + created_at / assignee.
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
			// Serves the admin financial reporting aggregations (profit report,
			// per-workspace snapshots, credits/refunds, product usage), which scan a
			// created_at range and net debit/refund-credit rows by workspace/service.
			// Covering columns let those SUM/GROUP BY run index-only without heap hits.
			name: "idx_bt_reporting",
			sql: `CREATE INDEX IF NOT EXISTS idx_bt_reporting
				ON balance_transactions (created_at)
				INCLUDE (workspace_id, service_type, type, is_refund, amount, cost_micros, profit_micros, exchange_rate_micros)`,
		},
		{
			// Serves workspace WhatsApp campaign "Envios" rollup (ledger-backed):
			// filter workspace + service + created_at range, then aggregate debits
			// and refunds. Covering columns avoid heap fetches for the GROUP BY.
			// Prefer this over the partial debit-only idx_bt_ws_created_svc because
			// the rollup must also read refund credits.
			name: "idx_bt_ws_svc_created_charges",
			sql: `CREATE INDEX IF NOT EXISTS idx_bt_ws_svc_created_charges
				ON balance_transactions (workspace_id, service_type, created_at)
				INCLUDE (type, is_refund, description, reference_id)
				WHERE service_type = 'whatsapp_campaign'`,
		},
		{
			// The reconcile sweep asks one question: which paid sends took money and
			// never reached a terminal state. Partial, because that set is tiny and
			// the table is dominated by settled rows.
			name: "idx_wats_unsettled",
			sql: `CREATE INDEX IF NOT EXISTS idx_wats_unsettled
				ON whatsapp_template_sends (updated_at)
				WHERE status IN ('pending', 'charged', 'unknown') AND deleted_at IS NULL`,
		},
		{
			// Delivery-status webhooks arrive knowing only Meta's message id.
			name: "idx_wats_wamid",
			sql: `CREATE INDEX IF NOT EXISTS idx_wats_wamid
				ON whatsapp_template_sends (workspace_id, provider_message_id)
				WHERE provider_message_id <> '' AND deleted_at IS NULL`,
		},
		{
			// Campaign list/summary filters by workspace + type + department while
			// joining from ledger reference_id → campaign id.
			name: "idx_wc_ws_type_dept",
			sql: `CREATE INDEX IF NOT EXISTS idx_wc_ws_type_dept
				ON whatsapp_campaigns (workspace_id, type, department_id)
				WHERE deleted_at IS NULL`,
		},
		{
			// Serves the "workspaces currently on plan X" subquery used to scope every
			// financial report by plan: filter by plan_definition_id (+status) and
			// return workspace_id index-only.
			name: "idx_ws_sub_plan_status_ws",
			sql: `CREATE INDEX IF NOT EXISTS idx_ws_sub_plan_status_ws
				ON workspace_subscriptions (plan_definition_id, status, workspace_id)`,
		},
		{
			// Serves the plan-contractions report: paid subscription invoices in a
			// created_at range, grouped by plan/cycle with revenue summed.
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
			name: "idx_cm_unread_inbound",
			sql: `CREATE INDEX IF NOT EXISTS idx_cm_unread_inbound
				ON conversation_messages (entry_id, entry_type)
				WHERE read = false AND deleted_at IS NULL
				AND message_type IN ('text', 'image', 'audio', 'user_message', 'media')`,
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
			// Entry-keyed lookup of active runs for the inbox enrichment
			// (FindActiveByEntries). Partial so it only indexes the few active runs,
			// keeping the batch conversation-page lookup an index scan at any scale.
			name: "idx_wfr_entry_active_only",
			sql: `CREATE INDEX IF NOT EXISTS idx_wfr_entry_active_only
				ON workflow_runs (entry_id, updated_at DESC)
				WHERE status IN ('running', 'waiting')`,
		},
		{
			// Trigger-scoped active-run lookup (FindActiveByEntryAndTrigger): keeps each
			// trigger of a multi-trigger workflow (e.g. message_received + webhook) an
			// independent single-run-per-entry lane. Partial so only active runs index.
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

		// Inbox/board listing (SearchEntriesByFilter). These back the last_message_at
		// ordering that replaced a per-entry JOIN LATERAL over conversation_messages.
		// Partial on "has messages", because only ~8% of entries ever get one: the
		// index stays a fraction of the table, and the planner no longer falls back to
		// a full scan of the (multi-GB) entries table on every inbox load.
		{
			name: "idx_wce_campaign_lastmsg",
			sql: `CREATE INDEX IF NOT EXISTS idx_wce_campaign_lastmsg
				ON whatsapp_campaign_entries (campaign_id, last_message_at DESC)
				WHERE deleted_at IS NULL AND last_message_at IS NOT NULL`,
		},
		{
			name: "idx_se_inbox_lastmsg",
			sql: `CREATE INDEX IF NOT EXISTS idx_se_inbox_lastmsg
				ON support_entries (inbox_id, last_message_at DESC)
				WHERE deleted_at IS NULL AND last_message_at IS NOT NULL`,
		},

		// Idle auto-close: open conversations ordered by last_agent_message_at.
		// Partial keeps the index tiny (only open + has agent reply). Planner uses
		// this for ListEligibleForAutoClose instead of scanning all entries.
		{
			name: "idx_wce_autoclose_agent",
			sql: `CREATE INDEX IF NOT EXISTS idx_wce_autoclose_agent
				ON whatsapp_campaign_entries (last_agent_message_at ASC)
				WHERE deleted_at IS NULL
				  AND conversation_status IN ('new', 'ongoing')
				  AND last_agent_message_at IS NOT NULL`,
		},
		// workspace_configs lookup by auto_close_enabled for join filter selectivity.
		{
			name: "idx_wsc_autoclose_enabled",
			sql: `CREATE INDEX IF NOT EXISTS idx_wsc_autoclose_enabled
				ON workspace_configs (workspace_id)
				WHERE auto_close_enabled = TRUE`,
		},
		// Max-age absolute inactivity (Policy C): open entries ordered by last_message_at.
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

		// The scheduled-message sweep's only query. Partial on status, so its
		// cost tracks the number of messages actually DUE rather than the size
		// of the table: a workspace with a million delivered rows still pays for
		// the handful that are pending.
		{
			name: "idx_sched_msg_due",
			sql: `CREATE INDEX IF NOT EXISTS idx_sched_msg_due
				ON scheduled_messages (scheduled_at)
				WHERE status = 'pending' AND deleted_at IS NULL`,
		},

		// The conversation's own scheduled messages, which the composer reads
		// every time an operator opens a chat.
		{
			name: "idx_sched_msg_entry_status",
			sql: `CREATE INDEX IF NOT EXISTS idx_sched_msg_entry_status
				ON scheduled_messages (entry_id, entry_type, status)
				WHERE deleted_at IS NULL`,
		},

		// Vector search for RAG. It lived at the end of the migration with its
		// error explicitly discarded, which was load-bearing: a failed statement
		// poisons a Postgres transaction, so anything added after it would have
		// failed with "current transaction is aborted" instead of its own error.
		// Best-effort belongs here, where nothing shares its transaction. The
		// vector extension and rag_chunks both exist by the time this runs.
		{
			name: "idx_rag_chunks_embedding_hnsw",
			sql: `CREATE INDEX IF NOT EXISTS idx_rag_chunks_embedding_hnsw
				ON rag_chunks USING hnsw (embedding vector_cosine_ops)
				WITH (m = 16, ef_construction = 64)`,
		},

		// Comment analysis. comment_analyses is a queue as well as a result
		// store, and every queue read is a partial index over the rows in one
		// status, so a workspace with a million analysed comments still pays
		// for the handful that are pending.

		// The flush job's claim: pending rows of one post, oldest first.
		{
			name: "idx_ca_pending",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_pending
				ON comment_analyses (source, account_id, container_id, created_at)
				WHERE status = 'pending' AND deleted_at IS NULL`,
		},
		// The backstop's reset of rows a dead replica left behind.
		{
			name: "idx_ca_inflight",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_inflight
				ON comment_analyses (updated_at)
				WHERE status = 'in_flight'`,
		},
		// Live stats and the feed: an account's analysed rows, newest first.
		{
			name: "idx_ca_stats",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_stats
				ON comment_analyses (workspace_id, account_id, analyzed_at DESC)
				WHERE status = 'analyzed' AND deleted_at IS NULL`,
		},
		// The flagged-comment slice: only rows at or above the high threshold.
		{
			name: "idx_ca_severity",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_severity
				ON comment_analyses (account_id, severity DESC)
				WHERE severity >= 60 AND deleted_at IS NULL`,
		},
		// The author projection rebuild and "this author's comments".
		{
			name: "idx_ca_author",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_author
				ON comment_analyses (source, account_id, author_external_id)`,
		},
		// The WINDOWED author ranking regroups the comments themselves rather
		// than reading the lifetime projection, so it filters on the account
		// and the comment's own timestamp before grouping by author. Without
		// commented_at in the key that filter is a scan of the account's whole
		// history to answer "esta semana".
		{
			name: "idx_ca_author_period",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_author_period
				ON comment_analyses (workspace_id, account_id, commented_at DESC, author_external_id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_ca_authors_rank",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_authors_rank
				ON comment_analysis_authors (source, account_id, is_flagged, high_sev_count DESC, max_severity DESC)`,
		},
		{
			name: "idx_ca_rollups_series",
			sql: `CREATE INDEX IF NOT EXISTS idx_ca_rollups_series
				ON comment_analysis_rollups (workspace_id, scope, scope_id, bucket_date)`,
		},
	}

	for _, idx := range indexes {
		if err := db.Exec(idx.sql).Error; err != nil {
			log.Printf("[indexes] Warning: failed to create %s: %v", idx.name, err)
		}
	}
}

// createSchemaConstraints creates the indexes that are part of the schema
// rather than tuning: the partial UNIQUE indexes GORM's struct tags cannot
// express.
//
// It is deliberately NOT part of CreatePerformanceIndexes, even though both now
// live in this file. A performance index that fails to build costs latency, so
// that path logs and continues. These ones ARE the rule, one bot per workspace,
// one open conversation per contact, one invoice per idempotency key, so a
// failure has to abort the migration. Demoting them to a logged warning would
// leave a database that accepts the duplicates they exist to reject.
//
// Runs inside the migration transaction, so the caller's advisory lock still
// serialises concurrent boots.
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
		// One default funnel per workspace and object kind.
		//
		// The application now enforces this in PromoteDefault, but the
		// application enforced nothing for months and five workspaces ended up
		// with two to four default funnels each, none of them deletable. This is
		// the constraint that makes the recurrence impossible rather than
		// unlikely. It builds only after
		// pl_demote_duplicate_default_pipelines has run, which is why repairs
		// come before constraints in RunMigrations.
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

		// The prompt block and the panel both read "this lead's memories,
		// newest first"; the partial index serves exactly that scan.
		{
			name: "idx_lead_memories_lead",
			sql: `CREATE INDEX IF NOT EXISTS idx_lead_memories_lead
				ON lead_memories (workspace_id, lead_id, created_at DESC)
				WHERE deleted_at IS NULL`,
		},
		{
			// Correlated subqueries on the leads list (memory count, last
			// memory, the free-text search, the memory-category facet) all
			// filter lm.lead_id = leads.id with NO workspace_id, so neither
			// index above can serve them: both lead with workspace_id, and a
			// composite index cannot be seeked on its second column. The
			// foreign key does not help either, since Postgres never indexes
			// the referencing side. Without this the list ran one sequential
			// scan of lead_memories per lead on screen.
			name: "idx_lead_memories_lead_id",
			sql: `CREATE INDEX IF NOT EXISTS idx_lead_memories_lead_id
				ON lead_memories (lead_id)
				WHERE deleted_at IS NULL`,
		},
		// One active memory per normalized content per lead. Partial on
		// deleted_at so forgetting a fact does not forbid re-learning it.
		{
			name: "ux_lead_memories_dedup",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_lead_memories_dedup
				ON lead_memories (workspace_id, lead_id, content_norm)
				WHERE deleted_at IS NULL`,
		},

		// An Instagram-scoped ID is unique only within the (app, professional
		// account) pair, so contact identity is (ig_account_id, igsid) and never
		// igsid alone.
		{
			name: "ux_ig_contact_account_igsid",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_ig_contact_account_igsid
				ON instagram_contacts (ig_account_id, igsid)
				WHERE deleted_at IS NULL`,
		},
		// One open conversation per (account, contact).
		{
			name: "ux_ig_conversation_account_contact",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_ig_conversation_account_contact
				ON instagram_conversations (ig_account_id, contact_id)
				WHERE deleted_at IS NULL`,
		},

		// One bot per workspace, globally. Mirrors the Instagram account and
		// WhatsApp phone rules: the same bot cannot be connected twice, and a
		// soft-deleted row must not block reconnecting it.
		{
			name: "ux_tg_account_bot_user",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tg_account_bot_user
				ON telegram_accounts (bot_user_id)
				WHERE deleted_at IS NULL`,
		},
		// A business connection identifies exactly one account: it is the ONLY
		// routing key a business-mode webhook carries, so two accounts claiming
		// one connection would make delivery ambiguous.
		{
			name: "ux_tg_account_business_connection",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tg_account_business_connection
				ON telegram_accounts (business_connection_id)
				WHERE business_connection_id IS NOT NULL AND deleted_at IS NULL`,
		},
		// A Telegram user id is global, but contact identity is still scoped to
		// the account so one workspace can never read another's row.
		{
			name: "ux_tg_contact_account_user",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tg_contact_account_user
				ON telegram_contacts (account_id, tg_user_id)
				WHERE deleted_at IS NULL`,
		},
		// One open conversation per (account, contact).
		{
			name: "ux_tg_conversation_account_contact",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tg_conversation_account_contact
				ON telegram_conversations (account_id, contact_id)
				WHERE deleted_at IS NULL`,
		},
		// The file cache is keyed by (account, object): "file_id is unique for
		// each individual bot and can't be transferred from one bot to another".
		{
			name: "ux_tg_file_cache_account_source",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tg_file_cache_account_source
				ON telegram_file_cache (account_id, source_key)`,
		},

		// One WhatsApp number connected once, globally. A number linked to two
		// workspaces would deliver every message to both, which is a tenancy
		// breach rather than a duplicate.
		{
			name: "ux_uw_instance_jid",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_instance_jid
				ON unofficial_whatsapp_instances (jid)
				WHERE jid <> '' AND deleted_at IS NULL`,
		},
		// The instance a webhook resolves to. Unique because the digest IS the
		// credential: two rows sharing one would make delivery ambiguous and the
		// tenancy check meaningless.
		{
			name: "ux_uw_instance_delivery_token",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_instance_delivery_token
				ON unofficial_whatsapp_instances (delivery_token_hash)
				WHERE deleted_at IS NULL`,
		},
		// One row per instance on a host, so a re-provision cannot orphan the
		// previous one behind a duplicate.
		{
			name: "ux_uw_instance_server_provider_id",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_instance_server_provider_id
				ON unofficial_whatsapp_instances (server_id, provider_instance_id)
				WHERE deleted_at IS NULL`,
		},
		// Contact identity is (instance, jid). A JID is global to WhatsApp, but
		// scoping to the instance is what stops one workspace reading another's
		// contact row for the same person.
		{
			name: "ux_uw_contact_instance_jid",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_contact_instance_jid
				ON unofficial_whatsapp_contacts (instance_id, jid)
				WHERE deleted_at IS NULL`,
		},
		// The LID is the second identifier WhatsApp uses for the same human.
		// Unique per instance so the reconciliation that merges the two forms
		// cannot itself create a duplicate.
		{
			name: "ux_uw_contact_instance_lid",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_contact_instance_lid
				ON unofficial_whatsapp_contacts (instance_id, lid)
				WHERE lid <> '' AND deleted_at IS NULL`,
		},
		// One conversation per (instance, contact): one real WhatsApp chat is
		// one CRM conversation, always.
		{
			name: "ux_uw_conversation_instance_contact",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_conversation_instance_contact
				ON unofficial_whatsapp_conversations (instance_id, contact_id)
				WHERE deleted_at IS NULL`,
		},
		// The same rule stated the other way round, and the one that actually
		// caught the bug: one conversation per CHAT.
		//
		// Both are needed because they fail differently. The subject index above
		// is satisfied by a group thread that has forked into one conversation
		// per participant — each row has a distinct contact — while this one
		// rejects it outright. It is the invariant the channel always claimed to
		// have ("one real WhatsApp chat is one CRM conversation") and never
		// enforced, and the merge in datarepairs.go exists to make it buildable
		// on a database that shipped without it.
		{
			name: "ux_uw_conversation_instance_chat",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_conversation_instance_chat
				ON unofficial_whatsapp_conversations (instance_id, chat_id)
				WHERE chat_id <> '' AND deleted_at IS NULL`,
		},

		// A group is identified by (instance, jid), for the same tenancy reason
		// contacts are: a JID is global to WhatsApp, and scoping to the instance
		// is what stops one workspace reading another's cached roster.
		{
			name: "ux_uw_group_instance_jid",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_group_instance_jid
				ON unofficial_whatsapp_groups (instance_id, jid)
				WHERE deleted_at IS NULL`,
		},
		// One roster row per member. The sync replaces a roster wholesale rather
		// than diffing it, and this is what makes a partially-failed replace
		// impossible to leave behind as a member listed twice.
		{
			name: "ux_uw_group_participant",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_group_participant
				ON unofficial_whatsapp_group_participants (group_id, jid)`,
		},

		// // One number per broadcast. A product rule rather than hygiene:
		// // receiving the same blast twice is the most common thing a recipient
		// // reports, and a report is what gets a number banned.
		// {
		// 	name: "ux_uw_broadcast_target_number",
		// 	sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_uw_broadcast_target_number
		// 		ON unofficial_whatsapp_broadcast_targets (broadcast_id, phone_number)`,
		// },

		// Durable duplicate protection for provider message ids. Webhook
		// delivery is at-least-once, and the Redis dedup guard has a 5-minute
		// TTL, so the database is the only thing that still rejects a replay
		// after eviction.
		//
		// Scoped to the ENTRY, like ux_sched_msg_idem below is scoped to the
		// workspace and for the same reason: the id is chosen by someone else,
		// so two conversations must not be able to collide on it. A replay is
		// the same message on the SAME entry; the same id on a different entry
		// is what happens when both ends of a chat are accounts we host — the
		// provider stamps one id, it arrives outbound on the sender's entry and
		// inbound on the receiver's, and the old key rejected the inbound copy.
		//
		// The predecessor is dropped first: it keyed on (entry_type,
		// external_message_id) and would keep rejecting those inbound copies at
		// insert time no matter what the read-before-insert check decides.
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

		// Create-idempotency for scheduled messages. Partial, because the vast
		// majority of rows carry no key and would otherwise all collide on NULL
		// (Postgres would allow that, but the index would be pure overhead).
		// Scoped to the workspace so two tenants cannot collide on a
		// client-chosen key.
		{
			name: "ux_sched_msg_idem",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_sched_msg_idem
				ON scheduled_messages (workspace_id, idempotency_key)
				WHERE idempotency_key IS NOT NULL AND deleted_at IS NULL`,
		},
		// Charge-idempotency for paid WhatsApp template sends. This is the single
		// mechanism that makes a retried request, a redelivered queue message and
		// a double-clicked button cost money once.
		//
		// It belongs HERE and not in CreatePerformanceIndexes: this is a rule, not
		// a speed-up, and a warning-only failure would leave a database that
		// happily accepts the duplicate charges the index exists to reject.
		//
		// Workspace-scoped for the same reason as the row above — the key is
		// chosen by the caller, and one tenant must not be able to collide with
		// another's by guessing it.
		{
			name: "ux_wa_tpl_send_idem",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_wa_tpl_send_idem
				ON whatsapp_template_sends (workspace_id, idempotency_key)
				WHERE idempotency_key IS NOT NULL AND deleted_at IS NULL`,
		},
		// One analysis per subject. This is what makes webhook redelivery free:
		// ingest is INSERT ... ON CONFLICT DO NOTHING against it, so a subject
		// delivered twice is classified (and billed) once.
		//
		// subject_kind is part of the key because the id spaces overlap: on
		// Instagram the same channel carries both comments and conversations,
		// and a conversation entry id colliding with a comment id would make
		// one of them silently un-ingestable. The older two-column index is
		// dropped first, since a unique index cannot be widened in place.
		{
			name: "ux_ca_subject_drop_legacy",
			sql:  `DROP INDEX IF EXISTS ux_ca_source_comment`,
		},
		{
			name: "ux_ca_subject",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_ca_subject
				ON comment_analyses (source, subject_kind, source_comment_id)`,
		},
		// One projection row per author per account, the upsert target of the
		// hourly rebuild.
		{
			name: "ux_ca_author",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_ca_author
				ON comment_analysis_authors (source, account_id, author_external_id)`,
		},
	}

	for _, c := range constraints {
		if err := tx.Exec(c.sql).Error; err != nil {
			// Named, because "duplicate key value violates unique constraint"
			// on its own does not say which rule the existing data breaks.
			return fmt.Errorf("creating constraint %s: %w", c.name, err)
		}
	}
	return nil
}
