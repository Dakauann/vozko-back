package conversation_repository

import (
	"testing"
	"time"

	"vozko/domain/conversation"
)

func TestFinishingAConversationEndsTheStoredWait(t *testing.T) {
	cases := []struct {
		name  string
		write conversation.StatusWrite
		ends  bool
	}{
		{"finishing with close provenance", conversation.StatusWrite{Status: conversation.ConversationStatusFinished, SetCloseMeta: true, ClosedAt: time.Now()}, true},
		{"finishing without provenance", conversation.StatusWrite{Status: conversation.ConversationStatusFinished}, true},
		{"reopening keeps the wait the customer started", conversation.StatusWrite{Status: conversation.ConversationStatusNew, ClearCloseMeta: true}, false},
		{"moving to ongoing keeps the wait", conversation.StatusWrite{Status: conversation.ConversationStatusOngoing}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			value, touched := StatusUpdates(c.write)["awaiting_reply_since"]
			if touched != c.ends {
				t.Fatalf("touches awaiting_reply_since = %v, want %v", touched, c.ends)
			}
			if touched && value != nil {
				t.Fatalf("awaiting_reply_since = %v, want NULL", value)
			}
		})
	}
}
