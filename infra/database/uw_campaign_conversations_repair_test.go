package database

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSplitConversationMergeKeepsEachCampaignsConversation(t *testing.T) {
	if !strings.Contains(uwDuplicateConversationsSQL, "PARTITION BY instance_id, chat_id, campaign_id") {
		t.Fatalf("duplicates must be per chat and campaign:\n%s", uwDuplicateConversationsSQL)
	}
}

func TestBackfillConversationCampaignsTakesTheLatestSentEntry(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(regexp.QuoteMeta("UPDATE unofficial_whatsapp_conversations AS c") +
		`.*SET campaign_id = latest\.campaign_id::text` +
		`.*ORDER BY e\.conversation_id, e\.sent_at DESC NULLS LAST, e\.updated_at DESC` +
		`.*AND c\.campaign_id = ''` +
		`.*AND c\.chat_id NOT LIKE '%@g\.us'`).
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

func TestBackfillConversationCampaignsLeavesGroupConversationsToTheGroup(t *testing.T) {
	tx := repairTx(t)
	for _, stmt := range []string{
		`CREATE SCHEMA uw_backfill_fixture`,
		`SET LOCAL search_path TO uw_backfill_fixture`,
		`CREATE TABLE unofficial_whatsapp_conversations (id uuid PRIMARY KEY, chat_id text NOT NULL, campaign_id text NOT NULL DEFAULT '')`,
		`CREATE TABLE unofficial_whatsapp_campaign_entries (id uuid PRIMARY KEY, campaign_id uuid NOT NULL, conversation_id uuid,
			sent_at timestamptz, updated_at timestamptz NOT NULL DEFAULT NOW(), deleted_at timestamptz)`,
		`INSERT INTO unofficial_whatsapp_conversations (id, chat_id) VALUES
			('11111111-1111-1111-1111-111111111111', '5511981124410@s.whatsapp.net'),
			('22222222-2222-2222-2222-222222222222', '120363429559044933@g.us')`,
		`INSERT INTO unofficial_whatsapp_campaign_entries (id, campaign_id, conversation_id, sent_at) VALUES
			(gen_random_uuid(), 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', '11111111-1111-1111-1111-111111111111', NOW()),
			(gen_random_uuid(), 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', '22222222-2222-2222-2222-222222222222', NOW())`,
	} {
		if err := tx.Exec(stmt).Error; err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}

	if err := backfillConversationCampaigns(tx); err != nil {
		t.Fatalf("repair failed: %v", err)
	}

	campaignOf := func(id string) string {
		var campaign string
		if err := tx.Raw(`SELECT campaign_id FROM unofficial_whatsapp_conversations WHERE id = ?`, id).Scan(&campaign).Error; err != nil {
			t.Fatal(err)
		}
		return campaign
	}
	if got := campaignOf("11111111-1111-1111-1111-111111111111"); got != "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" {
		t.Fatalf("lead conversation campaign = %q, want the campaign that reached it", got)
	}
	if got := campaignOf("22222222-2222-2222-2222-222222222222"); got != "" {
		t.Fatalf("group conversation campaign = %q, want it to stay the group's own conversation", got)
	}
}
