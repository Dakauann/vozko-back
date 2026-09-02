package conversation_repository

import (
	"strings"
	"testing"
)

// The entry-scoped channel registry. These pin the contract a descriptor
// must satisfy, so a channel added later inherits it instead of re-deriving
// it — the mistake that shipped three channels with a uuid/text cast bug and
// left only WhatsApp working.

// entryInfoSQL is COMPOSED from the descriptor rather than declared beside it,
// and these pin that it stays composed.
//
// Each channel used to carry a hand-written copy of this query that restated
// every projection sitting a few lines above it. Two spellings of one
// projection is two things to keep in step, and they did not stay in step:
// repointing the unofficial WhatsApp contact slot at its real lead needed the
// identical edit in both, and the conversation header went on answering with
// the old one until the second copy was found.
func TestEntryInfoSQLIsComposedFromTheDescriptor(t *testing.T) {
	for _, q := range channelQueries {
		sql := q.entryInfoSQL()

		// Every projection comes from the field the list paths read, so the two
		// cannot describe the same column differently.
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

		// The join chain is the same one both list paths use.
		if !strings.Contains(sql, q.entryJoinOn("?::uuid")) {
			t.Errorf("%s: entryInfoSQL does not reuse EntryJoin", q.EntryType)
		}

		// The caller binds exactly one value, the entry id.
		if got := strings.Count(sql, "?"); got != 1 {
			t.Errorf("%s: entryInfoSQL has %d bind parameters, want exactly 1",
				q.EntryType, got)
		}

		// Aliased exactly as GetEntryLastMessage scans them.
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
