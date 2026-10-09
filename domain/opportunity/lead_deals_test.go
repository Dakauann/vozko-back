package opportunity

import (
	"errors"
	"testing"
)

func TestTheLeadOfALinkedConversationDecidesTheDealsLead(t *testing.T) {
	cases := []struct {
		name    string
		given   string
		ofEntry string
		want    string
		err     error
	}{
		{"nothing given takes the conversation's lead", "", "lead-7", "lead-7", nil},
		{"the same lead is kept", "lead-7", "lead-7", "lead-7", nil},
		{"a conversation without a lead keeps the lead given", "lead-1", "", "lead-1", nil},
		{"neither side has a lead", "", "", "", nil},
		{"another lead than the conversation's is refused", "lead-1", "lead-7", "", ErrEntryLeadMismatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := LeadForLinkedEntry(c.given, c.ofEntry)
			if !errors.Is(err, c.err) || got != c.want {
				t.Fatalf("LeadForLinkedEntry(%q, %q) = %q, %v; want %q, %v", c.given, c.ofEntry, got, err, c.want, c.err)
			}
		})
	}
}
