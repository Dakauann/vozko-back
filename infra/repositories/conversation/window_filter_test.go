package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func TestEveryChannelDefinesItsWindowFilter(t *testing.T) {
	for _, q := range channelQueries {
		t.Run(string(q.EntryType), func(t *testing.T) {
			predicate := q.windowOpenOn("x.entry_id")
			if strings.TrimSpace(q.WindowOpenPredicate) == "" {
				t.Fatal("no window filter: the inbox would ignore the window filter for this channel")
			}
			if !strings.Contains(predicate, "= x.entry_id") {
				t.Fatalf("the filter is not tied to the row it filters: %s", predicate)
			}
			if strings.Contains(predicate, "?") {
				t.Fatalf("the filter binds placeholders it never receives: %s", predicate)
			}
			if strings.Contains(predicate, "%!") {
				t.Fatalf("the filter template is malformed: %s", predicate)
			}
			if !strings.Contains(predicate, "deleted_at IS NULL") {
				t.Fatalf("a deleted conversation could pass the filter: %s", predicate)
			}
		})
	}
}

func TestWorkspaceWindowFilterCoversEveryChannel(t *testing.T) {
	yes, no := true, false
	open, _ := workspaceInboxConditions(conversation.SearchEntriesInput{WorkspaceID: "ws-1", WindowOpen: &yes})
	closed, _ := workspaceInboxConditions(conversation.SearchEntriesInput{WorkspaceID: "ws-1", WindowOpen: &no})
	openSQL, closedSQL := strings.Join(open, " AND "), strings.Join(closed, " AND ")

	for _, q := range channelQueries {
		tied := "ae.entry_type = '" + string(q.EntryType) + "' AND " + q.windowOpenOn("ae.entry_id")
		if !strings.Contains(openSQL, tied) {
			t.Errorf("open filter skips %s:\n%s", q.EntryType, openSQL)
		}
		if !strings.Contains(closedSQL, tied) {
			t.Errorf("closed filter skips %s:\n%s", q.EntryType, closedSQL)
		}
	}
	if !strings.Contains(closedSQL, "NOT (") {
		t.Errorf("closed filter is not the negation of the open one:\n%s", closedSQL)
	}
}

func TestWorkspaceWindowFilterNarrowsToTheChosenChannel(t *testing.T) {
	yes := true
	conds, _ := workspaceInboxConditions(conversation.SearchEntriesInput{WorkspaceID: "ws-1", WindowOpen: &yes, EntryType: shared.EntryTypeTelegram})
	sql := strings.Join(conds, " AND ")
	telegram, _ := channelQueryFor(shared.EntryTypeTelegram)
	instagram, _ := channelQueryFor(shared.EntryTypeInstagram)
	if !strings.Contains(sql, telegram.windowOpenOn("ae.entry_id")) || strings.Contains(sql, instagram.windowOpenOn("ae.entry_id")) {
		t.Fatalf("filter = %s", sql)
	}
}

func TestContainerWindowFilterAppliesToEveryChannel(t *testing.T) {
	yes := true
	for _, q := range channelQueries {
		conds, _ := containerInboxConditions(conversation.SearchEntriesInput{CampaignID: "c-1", EntryType: q.EntryType, WindowOpen: &yes}, q, nil, true)
		if !strings.Contains(strings.Join(conds, " AND "), q.windowOpenOn("entries.entry_id")) {
			t.Errorf("%s container inbox ignores the window filter", q.EntryType)
		}
	}
}
