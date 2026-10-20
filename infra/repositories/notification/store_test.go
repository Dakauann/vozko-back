package notification_repository

import (
	"strings"
	"testing"
	"time"

	"vozko/domain/notification"
	"vozko/domain/shared"
)

func TestEveryStatementBindsAsManyArgumentsAsItHasPlaceholders(t *testing.T) {
	fixed := map[string]int{
		stalePendingSQL: 2, purgeSQL: 2, summarySQL: 3, markReadSQL: 4, markUnreadSQL: 3, markAllReadSQL: 3,
		membersSQL: 1, workspaceNameSQL: 1, workspaceIDsSQL: 1, workspaceIDsAfterSQL: 2,
		holdForDigestSQL: 2, heldWorkspacesSQL: 2, heldWorkspacesAfterSQL: 3, heldDigestsSQL: 3, releaseDigestsSQL: 3,
		broadcastStatsSQL: 1, withdrawSQL: 2,
		seeEpisodeSQL: 2, advanceEpisodeSQL: 5, closeEpisodeSQL: 3, resolveEpisodesSQL: 4, purgeEpisodesSQL: 2,
	}
	for statement, args := range fixed {
		if got := strings.Count(statement, "?"); got != args {
			t.Fatalf("%q has %d placeholders, binds %d", statement, got, args)
		}
	}
	for _, q := range []notification.RecipientQuery{
		{Status: notification.ReadStatusAll, Limit: 5},
		{Status: notification.ReadStatusRead, Paged: true, After: notification.RecipientCursor{NotificationID: "n", UserID: "u"}, Limit: 5},
	} {
		query, args := recipientsQuery("b", q)
		if strings.Count(query, "?") != len(args) {
			t.Fatalf("recipients query %+v binds %d args for %d placeholders", q, len(args), strings.Count(query, "?"))
		}
	}
	viewer := notification.Viewer{WorkspaceID: "w", UserID: "u"}
	for _, q := range []notification.ListQuery{
		{Limit: 5},
		{Limit: 5, Category: notification.CategoryBilling},
		{Limit: 5, UnreadOnly: true, Paged: true, Before: shared.Keyset{At: time.Now(), ID: "n"}},
		{Limit: 5, Category: notification.CategoryTeam, UnreadOnly: true, Paged: true, Before: shared.Keyset{At: time.Now(), ID: "n"}},
	} {
		query, args := inboxQuery(viewer, q)
		if strings.Count(query, "?") != len(args) {
			t.Fatalf("inbox query %+v binds %d args for %d placeholders", q, len(args), strings.Count(query, "?"))
		}
	}
}

func TestInvalidIdentifiersNeverReachTheDatabase(t *testing.T) {
	if validViewer(notification.Viewer{WorkspaceID: "ws", UserID: "u"}) {
		t.Fatal("non-uuid viewers must be refused before SQL")
	}
	if got := validUUIDs([]string{"x", " 6f1d1c1e-8f1a-4f7b-9a43-3e8c4f0c9b11 "}); len(got) != 1 || got[0] != "6f1d1c1e-8f1a-4f7b-9a43-3e8c4f0c9b11" {
		t.Fatalf("validUUIDs = %v", got)
	}
}
