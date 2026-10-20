package teamchat_repository

import (
	"strings"
	"testing"
)

func TestRawQueriesBindExactlyTheArgumentsTheyAreGiven(t *testing.T) {
	cases := map[string]struct {
		query string
		args  int
	}{
		"unread totals":   {unreadTotalsQuery, 3},
		"unread mentions": {unreadMentionsQuery, 3},
	}
	for name, c := range cases {
		if got := strings.Count(c.query, "?"); got != c.args {
			t.Errorf("%s binds %d placeholders, the call passes %d arguments", name, got, c.args)
		}
	}
}
