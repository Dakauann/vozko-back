package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/infra/database/sqlexpr"
)

func TestEveryChannelProjectsWhenTheCustomerAndTheTeamLastWrote(t *testing.T) {
	for _, ch := range channelQueries {
		t.Run(string(ch.EntryType), func(t *testing.T) {
			if ch.MessageClockTable == "" {
				t.Fatal("MessageClockTable is empty: the inbox would show no waiting time on this channel")
			}
			fields := ch.messageClockFieldsSQL()
			for _, column := range []string{"last_customer_message_at", "last_agent_message_at", "awaiting_reply_since"} {
				if !strings.Contains(fields, ch.MessageClockTable+"."+column+" AS "+column) {
					t.Errorf("does not read %s.%s:\n%s", ch.MessageClockTable, column, fields)
				}
			}
			if !strings.Contains(ch.entryInfoSQL(), fields) {
				t.Error("the live update does not project the message clocks the list does")
			}
			if !strings.Contains(ch.entryJoinOn("x"), " "+ch.MessageClockTable+" ON ") {
				t.Errorf("MessageClockTable %q is not the alias EntryJoin gives the conversation table", ch.MessageClockTable)
			}
		})
	}
}

func TestEveryWorkspaceSourceProjectsWhenTheCustomerStartedWaiting(t *testing.T) {
	for _, src := range entrySources {
		t.Run(string(src.EntryType), func(t *testing.T) {
			ch, ok := channelQueryFor(src.EntryType)
			if !ok {
				t.Fatal("a workspace source without a channel query cannot be sorted by waiting time")
			}
			sql, _ := src.inboxSelect(entrySourceScope{}, "ws-1")
			want := sqlexpr.WaitingSince(ch.MessageClockTable) + " AS ws_at"
			if !strings.Contains(sql, want) {
				t.Errorf("inboxSelect is missing %q:\n%s", want, sql)
			}
			if !strings.Contains(sql, " "+ch.MessageClockTable+" ") {
				t.Errorf("the source does not alias its conversation table %s:\n%s", ch.MessageClockTable, sql)
			}
		})
	}
}

func TestTheContainerInboxSortsByTheSameWaitingRule(t *testing.T) {
	for _, ch := range channelQueries {
		t.Run(string(ch.EntryType), func(t *testing.T) {
			in := conversation.SearchEntriesInput{EntryType: ch.EntryType, CampaignID: "c-1", SortOrder: conversation.SortOrderWaiting}
			q := containerInboxQueries(in, ch, nil, 20, 0)
			if !strings.Contains(q.pageSQL, sqlexpr.WaitingSince(ch.MessageClockTable)+" AS ws_at") {
				t.Errorf("the container page does not project the waiting rule:\n%s", q.pageSQL)
			}
			if !strings.Contains(q.pageSQL, "matching.ws_at ASC NULLS LAST") {
				t.Errorf("the container page does not order by the waiting rule:\n%s", q.pageSQL)
			}
		})
	}
}
