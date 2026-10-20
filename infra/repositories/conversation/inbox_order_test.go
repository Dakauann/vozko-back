package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/personalinbox"
)

var workspaceOrderColumns = inboxOrderColumns{LastMessage: "lm_created_at", WaitingSince: "ws_at", Pinned: "pinned"}

func TestInboxOrderKeepsTodaysOrderWhenNothingPersonalApplies(t *testing.T) {
	cols := workspaceOrderColumns
	cols.Pinned = ""
	cases := map[string]string{
		conversation.SortOrderRecent: "ORDER BY lm_created_at DESC",
		"desc":                       "ORDER BY lm_created_at DESC",
		"asc":                        "ORDER BY lm_created_at ASC",
	}
	for sortOrder, want := range cases {
		if got := inboxOrderBySQL(sortOrder, cols); got != want {
			t.Errorf("inboxOrderBySQL(%q) = %q, want %q", sortOrder, got, want)
		}
	}
}

func TestInboxOrderPutsPinnedFirstThenTheChosenSort(t *testing.T) {
	recent := inboxOrderBySQL(conversation.SortOrderRecent, workspaceOrderColumns)
	if recent != "ORDER BY pinned DESC, lm_created_at DESC" {
		t.Errorf("recent with pins = %q", recent)
	}
	waiting := inboxOrderBySQL(conversation.SortOrderWaiting, workspaceOrderColumns)
	want := "ORDER BY pinned DESC, ws_at ASC NULLS LAST, lm_created_at DESC"
	if waiting != want {
		t.Errorf("waiting =\n%s\nwant\n%s", waiting, want)
	}
}

func TestPersonalScopeJoinsOnlyTheRequestersMarks(t *testing.T) {
	scope := personalScope{view: personalinbox.ViewInbox, userID: "user-1"}
	join, args := scope.join("ae.entry_id", "ae.entry_type")
	if strings.Count(join, "?") != len(args) || len(args) != 1 || args[0] != "user-1" {
		t.Fatalf("join %q binds %v", join, args)
	}
	if !strings.Contains(join, "pm.user_id = ?") || !strings.Contains(join, "pm.entry_id = ae.entry_id") || !strings.Contains(join, "pm.entry_type = ae.entry_type") {
		t.Fatalf("join does not match the user, the conversation and its channel: %q", join)
	}
}

func TestPersonalScopeConditionsPerView(t *testing.T) {
	cases := []struct {
		name  string
		scope personalScope
		want  string
	}{
		{"surfaces that did not ask see everything", personalScope{view: personalinbox.ViewNone, userID: "u"}, ""},
		{"the inbox shows every conversation, pins only order it", personalScope{view: personalinbox.ViewInbox, userID: "u"}, ""},
		{"pinned shows only the requester's pins", personalScope{view: personalinbox.ViewPinned, userID: "u"}, "pm.pinned_at IS NOT NULL"},
		{"an inbox without a user ignores marks", personalScope{view: personalinbox.ViewInbox}, ""},
		{"a pinned view without a user shows nothing", personalScope{view: personalinbox.ViewPinned}, "1 = 0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.scope.condition(); got != c.want {
				t.Fatalf("condition = %q, want %q", got, c.want)
			}
		})
	}
}

func TestLabelConditionBindsOnlyTheLabel(t *testing.T) {
	sql, args := labelCondition("label-1", "ae.entry_id", "ae.entry_type")
	if strings.Count(sql, "?") != len(args) || len(args) != 1 || args[0] != "label-1" {
		t.Fatalf("%q binds %v", sql, args)
	}
	for _, part := range []string{"el.entry_id = ae.entry_id", "el.entry_type = ae.entry_type", "el.deleted_at IS NULL"} {
		if !strings.Contains(sql, part) {
			t.Errorf("label condition is missing %q: %s", part, sql)
		}
	}
}
