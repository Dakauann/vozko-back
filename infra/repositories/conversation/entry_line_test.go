package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/shared"
)

func TestEveryChannelNamesTheLineItsConversationsRunOn(t *testing.T) {
	for _, q := range channelQueries {
		if strings.TrimSpace(q.LineField) == "" {
			t.Errorf("%s: no LineField, so its conversations could not say which line they run on", q.EntryType)
		}
	}
}

func TestTheEntryLineIsReadThroughTheInboxJoinOfEachChannel(t *testing.T) {
	sql := EntryLineSQL("le.entry_type", "le.entry_id")
	for _, q := range channelQueries {
		branch := "WHEN '" + string(q.EntryType) + "' THEN (SELECT " + q.LineField + " FROM (SELECT 1) AS line_anchor " + q.entryJoinOn("le.entry_id") + " LIMIT 1)"
		if !strings.Contains(sql, branch) {
			t.Errorf("%s: the line is not read through the inbox EntryJoin:\n%s", q.EntryType, sql)
		}
	}
	if !strings.HasPrefix(sql, "COALESCE(CASE le.entry_type ") || !strings.HasSuffix(sql, " END, '')") {
		t.Fatalf("an unknown channel must read as no line, never as NULL: %s", sql)
	}
}

func TestTheEntryLineBindsNothing(t *testing.T) {
	if got := strings.Count(EntryLineSQL("le.entry_type", "le.entry_id"), "?"); got != 0 {
		t.Fatalf("the line expression has %d placeholders; it is embedded in other raw queries and must bind none", got)
	}
}

func TestAnOfficialConversationNamesThePhoneThatReceivedItBeforeTheCampaignPhone(t *testing.T) {
	q, _ := channelQueryFor(shared.EntryTypeWhatsApp)
	if !strings.Contains(q.LineField, "COALESCE(wce.received_business_phone_id, wc.business_phone_id)") {
		t.Fatalf("official line must follow the phone the conversation ran on: %s", q.LineField)
	}
}
