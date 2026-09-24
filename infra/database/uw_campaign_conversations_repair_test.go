package database

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSplitConversationMergeKeepsEachCampaignsConversation(t *testing.T) {
	// The merge runs on every boot. Keyed on the chat alone it would fold each
	// campaign's conversation back into one and undo the official-like split.
	if !strings.Contains(uwDuplicateConversationsSQL, "PARTITION BY instance_id, chat_id, campaign_id") {
		t.Fatalf("duplicates must be per chat and campaign:\n%s", uwDuplicateConversationsSQL)
	}
}

func TestBackfillConversationCampaignsTakesTheLatestSentEntry(t *testing.T) {
	// Existing conversations keep the campaign the inbox already shows for them:
	// the latest entry sent into them. Rows already keyed are left alone, so the
	// repair is safe on every boot.
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(regexp.QuoteMeta("UPDATE unofficial_whatsapp_conversations AS c") +
		`.*SET campaign_id = latest\.campaign_id::text` +
		`.*ORDER BY e\.conversation_id, e\.sent_at DESC NULLS LAST, e\.updated_at DESC` +
		`.*AND c\.campaign_id = ''`).
		WillReturnResult(sqlmock.NewResult(0, 3))

	if err := backfillConversationCampaigns(db); err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBackfillConversationCampaignsIsRegistered(t *testing.T) {
	for _, name := range repairNames() {
		if name == "uw_backfill_conversation_campaigns" {
			return
		}
	}
	t.Fatal("the conversation campaign backfill is not registered in runDataRepairs")
}
