package sqlexpr

import (
	"strings"
	"testing"
)

func TestWaitingSinceMirrorsTheDomainRule(t *testing.T) {
	got := WaitingSince("tgc")
	want := "CASE WHEN tgc.conversation_status = 'finished' THEN NULL ELSE tgc.awaiting_reply_since END"
	if got != want {
		t.Fatalf("WaitingSince = %q\nwant          %q", got, want)
	}
}

func TestTheFirstUnansweredCustomerMessageComesAfterTheLastReply(t *testing.T) {
	got := FirstUnansweredCustomerMessage("c.id", "'telegram'")
	for _, part := range []string{
		"MIN(fu.created_at)",
		"fu.entry_id = c.id AND fu.entry_type = 'telegram'",
		"fu.deleted_at IS NULL",
		SentByContact("fu"),
		"MAX(lr.created_at)",
		"lr.entry_id = c.id AND lr.entry_type = 'telegram'",
		"lr.deleted_at IS NULL",
		SentAsReply("lr"),
		"fu.created_at > COALESCE(",
	} {
		if !strings.Contains(got, part) {
			t.Errorf("missing %q in\n%s", part, got)
		}
	}
	if strings.Contains(got, "?") {
		t.Fatalf("binds nothing, so it must hold no placeholder: %s", got)
	}
}
