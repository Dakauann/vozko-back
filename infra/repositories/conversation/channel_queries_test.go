package conversation_repository

import (
	"strings"
	"testing"
)

func TestEntryInfoSQLIsComposedFromTheDescriptor(t *testing.T) {
	for _, q := range channelQueries {
		sql := q.entryInfoSQL()

		for name, field := range map[string]string{
			"contact slot":       contactRefText(q.EntryType),
			"AccountIDField":     q.AccountIDField,
			"ContainerIDField":   q.ContainerIDField,
			"ContainerNameField": q.ContainerNameField,
			"AutomationFields":   q.AutomationFields,
			"AutomationColumn":   q.AutomationColumn,
		} {
			if field == "" {
				continue
			}
			if !strings.Contains(sql, field) {
				t.Errorf("%s: entryInfoSQL does not use %s (%q); it has drifted "+
					"into a second spelling", q.EntryType, name, field)
			}
		}

		if !strings.Contains(sql, q.entryJoinOn("?::uuid")) {
			t.Errorf("%s: entryInfoSQL does not reuse EntryJoin", q.EntryType)
		}

		if got := strings.Count(sql, "?"); got != 1 {
			t.Errorf("%s: entryInfoSQL has %d bind parameters, want exactly 1",
				q.EntryType, got)
		}

		for _, alias := range []string{
			"AS lead_id", "AS business_phone_id", "AS campaign_id",
			"AS campaign_name", "AS agent_id", "AS workflow_id",
			"AS agent_responses_enabled", "AS workflow_enabled",
			"AS automation_enabled",
		} {
			if !strings.Contains(sql, alias) {
				t.Errorf("%s: entryInfoSQL is missing %q; the scan is by column "+
					"name and would silently return a zero value", q.EntryType, alias)
			}
		}
	}
}
