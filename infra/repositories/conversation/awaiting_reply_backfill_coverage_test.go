package conversation_repository

import (
	"testing"

	"vozko/infra/database"
)

func TestTheBootBackfillCoversEveryInboxChannel(t *testing.T) {
	tables := database.AwaitingReplyBackfillTables()
	for _, ch := range channelQueries {
		if tables[ch.EntryType] != ch.EntryTable {
			t.Errorf("%s: the backfill writes %q, the inbox reads %q", ch.EntryType, tables[ch.EntryType], ch.EntryTable)
		}
	}
	if len(tables) != len(channelQueries) {
		t.Errorf("the backfill covers %d channels, the inbox lists %d", len(tables), len(channelQueries))
	}
}
