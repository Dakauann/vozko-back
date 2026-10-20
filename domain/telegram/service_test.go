package telegram

import "testing"

func TestAllowedUpdatesSubscribesToCustomerReactions(t *testing.T) {
	have := map[string]bool{}
	for _, u := range AllowedUpdates() {
		have[u] = true
	}
	for _, r := range []string{"message", "message_reaction", "my_chat_member", "business_connection", "business_message"} {
		if !have[r] {
			t.Errorf("allowed_updates is missing %q", r)
		}
	}
	if have["message_reaction_count"] {
		t.Error("message_reaction_count carries anonymous totals with no reactor, the inbox cannot attribute them")
	}
}

func TestSubscribesToAllowedUpdatesDetectsABotRegisteredWithAnOlderList(t *testing.T) {
	current := AllowedUpdates()
	reversed := make([]string, len(current))
	for i, u := range current {
		reversed[len(current)-1-i] = u
	}
	cases := []struct {
		name       string
		subscribed []string
		want       bool
	}{
		{"the current list is in sync", current, true},
		{"order does not matter to Telegram", reversed, true},
		{"a list registered before reactions existed is stale", withoutUpdate(current, "message_reaction"), false},
		{"an extra update type is stale", append(append([]string{}, current...), "poll_answer"), false},
		{"no list means Telegram's default set, which is not ours", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SubscribesToAllowedUpdates(tc.subscribed); got != tc.want {
				t.Fatalf("SubscribesToAllowedUpdates(%v) = %t, want %t", tc.subscribed, got, tc.want)
			}
		})
	}
}

func withoutUpdate(list []string, drop string) []string {
	out := make([]string, 0, len(list))
	for _, u := range list {
		if u != drop {
			out = append(out, u)
		}
	}
	return out
}
