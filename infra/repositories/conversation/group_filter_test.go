package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func TestUnofficialWhatsAppTellsGroupConversationsApart(t *testing.T) {
	q, ok := channelQueryFor(shared.EntryTypeUnofficialWhatsApp)
	if !ok {
		t.Fatal("no unofficial WhatsApp channel query")
	}
	predicate := q.groupOn("x.entry_id")
	for _, want := range []string{"= x.entry_id", "is_group", "deleted_at IS NULL"} {
		if !strings.Contains(predicate, want) {
			t.Fatalf("group predicate misses %q: %s", want, predicate)
		}
	}
	if strings.Contains(predicate, "?") || strings.Contains(predicate, "%!") {
		t.Fatalf("group predicate is malformed: %s", predicate)
	}
}

func TestWorkspaceGroupFilterKeepsOnlyGroupsOrDropsThem(t *testing.T) {
	yes, no := true, false
	groups, _ := workspaceInboxConditions(conversation.SearchEntriesInput{WorkspaceID: "ws-1", IsGroup: &yes})
	people, _ := workspaceInboxConditions(conversation.SearchEntriesInput{WorkspaceID: "ws-1", IsGroup: &no})
	q, _ := channelQueryFor(shared.EntryTypeUnofficialWhatsApp)
	tied := "ae.entry_type = '" + string(shared.EntryTypeUnofficialWhatsApp) + "' AND " + q.groupOn("ae.entry_id")

	groupsSQL, peopleSQL := strings.Join(groups, " AND "), strings.Join(people, " AND ")
	if !strings.Contains(groupsSQL, tied) {
		t.Errorf("groups filter does not keep unofficial WhatsApp groups:\n%s", groupsSQL)
	}
	if !strings.Contains(peopleSQL, "NOT (("+tied) {
		t.Errorf("the people filter is not the negation of the groups one:\n%s", peopleSQL)
	}
}

func TestWorkspaceGroupFilterFindsNothingOnAChannelWithoutGroups(t *testing.T) {
	yes := true
	conds, _ := workspaceInboxConditions(conversation.SearchEntriesInput{WorkspaceID: "ws-1", IsGroup: &yes, EntryType: shared.EntryTypeTelegram})
	if sql := strings.Join(conds, " AND "); !strings.Contains(sql, "(1 = 0)") {
		t.Fatalf("a groups filter on a channel without groups must match nothing:\n%s", sql)
	}
}

func TestContainerGroupFilterAppliesToEveryChannel(t *testing.T) {
	yes, no := true, false
	for _, q := range channelQueries {
		groups, _ := containerInboxConditions(conversation.SearchEntriesInput{CampaignID: "c-1", EntryType: q.EntryType, IsGroup: &yes}, q, nil, true)
		people, _ := containerInboxConditions(conversation.SearchEntriesInput{CampaignID: "c-1", EntryType: q.EntryType, IsGroup: &no}, q, nil, true)
		groupsSQL, peopleSQL := strings.Join(groups, " AND "), strings.Join(people, " AND ")
		if q.GroupPredicate == "" {
			if !strings.Contains(groupsSQL, "1 = 0") {
				t.Errorf("%s has no groups, so a groups filter must match nothing:\n%s", q.EntryType, groupsSQL)
			}
			continue
		}
		if !strings.Contains(groupsSQL, q.groupOn("entries.entry_id")) || !strings.Contains(peopleSQL, "NOT ("+q.groupOn("entries.entry_id")) {
			t.Errorf("%s container inbox ignores the group filter:\n%s\n%s", q.EntryType, groupsSQL, peopleSQL)
		}
	}
}
