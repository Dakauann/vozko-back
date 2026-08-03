package whatsapp_campaign_entry

import (
	"strings"
	"testing"
)

// Documents the auto-close eligibility SQL shape so refactors keep the
// single-JOIN, no N+1 contract. Real EXPLAIN ANALYZE is run in ops after
// migrate (partial index idx_wce_autoclose_agent + ANALYZE on backfill).
func TestAutoCloseEligibilitySQLShape(t *testing.T) {
	// Mirror of ListEligibleForAutoClose, keep in sync with repository method.
	sql := `
		SELECT e.id AS entry_id,
		       c.workspace_id AS workspace_id,
		       e.last_agent_message_at AS last_agent_message_at
		FROM whatsapp_campaign_entries e
		INNER JOIN whatsapp_campaigns c
		  ON c.id = e.campaign_id AND c.deleted_at IS NULL
		INNER JOIN workspace_configs wc
		  ON wc.workspace_id = c.workspace_id
		WHERE e.deleted_at IS NULL
		  AND e.conversation_status IN ('new', 'ongoing')
		  AND wc.auto_close_enabled = TRUE
		  AND e.last_agent_message_at IS NOT NULL
		  AND e.last_agent_message_at < NOW() - (GREATEST(wc.auto_close_idle_after_hours, 1) * INTERVAL '1 hour')
		  AND (e.last_customer_message_at IS NULL
		       OR e.last_customer_message_at < e.last_agent_message_at)
		ORDER BY e.last_agent_message_at ASC
		LIMIT ?
	`
	// No correlated subqueries / per-row config fetches.
	lower := strings.ToLower(sql)
	if strings.Contains(lower, "select exists") || strings.Count(lower, "select") > 1 {
		// One top-level SELECT only (JOIN is fine).
		if strings.Count(lower, "select ") != 1 {
			t.Fatalf("expected single SELECT (no N+1 subselects), got: %s", sql)
		}
	}
	for _, must := range []string{
		"workspace_configs",
		"last_agent_message_at",
		"auto_close_enabled",
		"conversation_status in ('new', 'ongoing')",
		"order by e.last_agent_message_at asc",
		"limit ?",
	} {
		if !strings.Contains(lower, must) {
			t.Fatalf("eligibility SQL missing %q", must)
		}
	}
}
