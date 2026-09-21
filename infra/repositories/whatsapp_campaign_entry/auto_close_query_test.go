package whatsapp_campaign_entry

import (
	"strings"
	"testing"
)

func TestAutoCloseEligibilitySQLShape(t *testing.T) {
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
	lower := strings.ToLower(sql)
	if strings.Contains(lower, "select exists") || strings.Count(lower, "select") > 1 {
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
