package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/shared"
)

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

func TestAChannelWithoutStatusStillProjectsTheColumn(t *testing.T) {
	q := channelQuery{}
	if got := q.statusColumnOrEmpty(); got != "''::text" {
		t.Errorf("statusColumnOrEmpty() = %q, want an empty string literal", got)
	}
}
