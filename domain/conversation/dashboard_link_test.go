package conversation

import (
	"testing"

	"vozko/domain/shared"
)

func TestDashboardLinkOpensTheConversationTheWayTheFrontReadsIt(t *testing.T) {
	cases := []struct {
		entry shared.EntryRef
		want  string
	}{
		{shared.EntryRef{EntryID: "3f1c", EntryType: shared.EntryTypeInstagram}, "/dashboard/live-chat?entry=3f1c&type=instagram"},
		{shared.EntryRef{EntryID: "a b&c", EntryType: shared.EntryTypeWebchat}, "/dashboard/live-chat?entry=a+b%26c&type=webchat"},
	}
	for _, c := range cases {
		if got := DashboardLink(c.entry); got != c.want {
			t.Errorf("DashboardLink(%+v) = %q, want %q", c.entry, got, c.want)
		}
	}
}
