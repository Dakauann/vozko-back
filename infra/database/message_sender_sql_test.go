package database

import "testing"

func TestMessageSenderPredicates(t *testing.T) {
	for name, tc := range map[string]struct {
		got  string
		want string
	}{
		"from the contact":        {SentByContactSQL("cm"), "cm.sender_kind = 'contact'"},
		"a reply to the customer": {SentAsReplySQL("cm"), "cm.sender_kind IN ('human', 'ai', 'workflow', 'external')"},
		"anything we sent":        {SentOutboundSQL("cm"), "cm.sender_kind IN ('human', 'ai', 'workflow', 'campaign', 'external')"},
		"without an alias":        {SentByContactSQL(""), "sender_kind = 'contact'"},
	} {
		t.Run(name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}
