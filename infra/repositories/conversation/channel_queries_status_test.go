package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/shared"
)

// Every registered channel must project its conversation status.
//
// The inbox reads this fact off the row for all of them. It used to be resolved
// only from the official WhatsApp entry repository, so every other channel's
// row was built with the zero value and the inbox rendered "Nova" over a
// conversation the database had as ongoing: replying appeared to move the
// conversation backwards.
//
// Structural rather than a list of channels on purpose. A channel added to the
// registry without a status column fails here instead of shipping the same bug
// a fourth time.
func TestEveryChannelProjectsItsConversationStatus(t *testing.T) {
	for _, entryType := range []shared.EntryType{
		shared.EntryTypeWhatsApp,
		shared.EntryTypeInstagram,
		shared.EntryTypeTelegram,
		shared.EntryTypeUnofficialWhatsApp,
	} {
		t.Run(string(entryType), func(t *testing.T) {
			q, ok := channelQueryFor(entryType)
			if !ok {
				t.Fatalf("%s is not a registered channel", entryType)
			}
			if q.StatusColumn == "" {
				t.Fatalf("%s declares no status column, so its inbox rows render as Nova whatever the database says", entryType)
			}
			sql := q.entryInfoSQL()
			if !strings.Contains(sql, "AS conversation_status") {
				t.Errorf("%s does not project conversation_status:\n%s", entryType, sql)
			}
			if !strings.Contains(sql, q.StatusColumn) {
				t.Errorf("%s projects a status that is not its own column %q", entryType, q.StatusColumn)
			}
		})
	}
}

// A channel that genuinely has no status (support) must still produce a
// well-formed projection rather than invalid SQL with a hole in it.
func TestAChannelWithoutStatusStillProjectsTheColumn(t *testing.T) {
	q := channelQuery{}
	if got := q.statusColumnOrEmpty(); got != "''::text" {
		t.Errorf("statusColumnOrEmpty() = %q, want an empty string literal", got)
	}
}
