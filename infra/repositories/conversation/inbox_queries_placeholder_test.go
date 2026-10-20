package conversation_repository

import (
	"strings"
	"testing"
	"time"

	"vozko/domain/actor"
	"vozko/domain/conversation"
	"vozko/domain/personalinbox"
	"vozko/domain/shared"
)

func everyInboxFilter(base conversation.SearchEntriesInput) []conversation.SearchEntriesInput {
	yes, no, two := true, false, 2
	from, to := time.Now().Add(-time.Hour), time.Now()
	full := base
	full.Query = "maria"
	full.StageIDs, full.StageWorkspaceID = []string{"s1"}, "ws-1"
	full.LabelID = "label-1"
	full.HasUnread = &yes
	full.WindowOpen = &no
	full.Channel = "whatsapp"
	full.MessageSearch = "pedido"
	full.AssignedUserID = "user-1"
	full.ResponsibleKind = actor.KindAI
	full.MinMessageCount, full.MaxMessageCount = &two, &two
	full.DateFrom, full.DateTo = &from, &to
	full.ConversationStatus = conversation.ConversationStatusOngoing
	full.DepartmentIDs, full.RestrictDepartments, full.AssigneeOverrideUserID = []string{"d1"}, true, "user-1"

	var out []conversation.SearchEntriesInput
	for _, view := range []personalinbox.View{personalinbox.ViewNone, personalinbox.ViewInbox, personalinbox.ViewPinned} {
		for _, sortOrder := range []string{conversation.SortOrderRecent, conversation.SortOrderWaiting} {
			for _, in := range []conversation.SearchEntriesInput{base, full} {
				in.Personal, in.PersonalUserID, in.SortOrder = view, "user-1", sortOrder
				out = append(out, in)
			}
		}
	}
	unstaged := full
	unstaged.StageIDs = []string{"__unstaged__"}
	unstaged.Query, unstaged.ResponsibleKind, unstaged.ResponsibleUserID = "", "", "user-2"
	return append(out, unstaged)
}

func assertBound(t *testing.T, name, sql string, args []interface{}) {
	t.Helper()
	if got := strings.Count(sql, "?"); got != len(args) {
		t.Errorf("%s has %d placeholders but binds %d arguments", name, got, len(args))
	}
}

func TestWorkspaceInboxQueriesBindEveryPlaceholder(t *testing.T) {
	for i, in := range everyInboxFilter(conversation.SearchEntriesInput{WorkspaceID: "ws-1"}) {
		q, ok := workspaceInboxQueries(in, 20, 40)
		if !ok {
			t.Fatalf("case %d built no query", i)
		}
		assertBound(t, "count", q.countSQL, q.countArgs)
		assertBound(t, "page", q.pageSQL, q.pageArgs)
	}
}

func TestContainerInboxQueriesBindEveryPlaceholder(t *testing.T) {
	for _, ch := range channelQueries {
		bases := []conversation.SearchEntriesInput{
			{CampaignID: "c-1", EntryType: ch.EntryType},
			{EntryIDs: []string{"e-1"}, EntryType: ch.EntryType},
		}
		if ch.CampaignCTE != "" {
			bases = append(bases, conversation.SearchEntriesInput{CampaignID: "c-1", EntryType: ch.EntryType, ContainerKind: conversation.ContainerKindCampaign})
		}
		for _, base := range bases {
			for _, in := range everyInboxFilter(base) {
				q := containerInboxQueries(in, ch, in.EntryIDs, 20, 0)
				assertBound(t, string(ch.EntryType)+" count", q.countSQL, q.countArgs)
				assertBound(t, string(ch.EntryType)+" page", q.pageSQL, q.pageArgs)
			}
		}
	}
}

func TestInboxQueriesJoinMarksOnlyWhenAPersonalViewIsAsked(t *testing.T) {
	plain := conversation.SearchEntriesInput{WorkspaceID: "ws-1", PersonalUserID: "user-1"}
	q, _ := workspaceInboxQueries(plain, 20, 0)
	if strings.Contains(q.pageSQL, "conversation_personal_marks") {
		t.Error("a surface that never asked for personal marks (funnel, board, Elo) must not read them")
	}
	inbox := plain
	inbox.Personal = personalinbox.ViewInbox
	q, _ = workspaceInboxQueries(inbox, 20, 0)
	if !strings.Contains(q.pageSQL, "conversation_personal_marks") || !strings.Contains(q.pageSQL, "ORDER BY pinned DESC") {
		t.Errorf("the inbox must join the requester's marks and put pins first:\n%s", q.pageSQL)
	}

	container := conversation.SearchEntriesInput{CampaignID: "c-1", EntryType: shared.EntryTypeTelegram, Personal: personalinbox.ViewInbox, PersonalUserID: "user-1"}
	ch, _ := channelQueryFor(shared.EntryTypeTelegram)
	cq := containerInboxQueries(container, ch, nil, 20, 0)
	if !strings.Contains(cq.pageSQL, "pm.entry_type = 'telegram'::text") || !strings.Contains(cq.pageSQL, "ORDER BY matching.pinned DESC") {
		t.Errorf("the container inbox must join marks of its own channel and put pins first:\n%s", cq.pageSQL)
	}
}
