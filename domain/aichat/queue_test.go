package aichat

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestClientIDsAreShortOpaqueTokens(t *testing.T) {
	cases := []struct {
		id    string
		valid bool
	}{
		{"0f8fad5b-d9cb-469f-a165-70867728950e", true},
		{"q_1", true},
		{"", false},
		{"   ", false},
		{"has space", false},
		{"semi;colon", false},
		{strings.Repeat("a", MaxClientIDLen), true},
		{strings.Repeat("a", MaxClientIDLen+1), false},
	}
	for _, c := range cases {
		err := ValidClientID(c.id)
		if c.valid && err != nil {
			t.Errorf("%q rejected: %v", c.id, err)
		}
		if !c.valid && !errors.Is(err, ErrInvalidClientID) {
			t.Errorf("%q accepted", c.id)
		}
	}
}

func TestAQueuedMessageBecomesAUserMessageOfItsThread(t *testing.T) {
	q := QueuedMessage{ThreadID: "t1", ClientID: "c1", Content: "e o vídeo?", Attachments: []byte(`[{"mediaId":"m1"}]`)}
	m := q.Message()
	if m.ThreadID != "t1" || m.Role != RoleUser || m.Content != "e o vídeo?" || string(m.Attachments) != `[{"mediaId":"m1"}]` {
		t.Fatalf("message = %+v", m)
	}
	if m.ID != "" || m.ProposalID != "" {
		t.Fatalf("the message must be new and carry no proposal: %+v", m)
	}
}

func TestOnlyAMessageWithADeliveryStampCountsAsDelivered(t *testing.T) {
	q := QueuedMessage{}
	if q.Delivered() {
		t.Fatal("a fresh queued message is not delivered")
	}
	now := time.Now()
	q.DeliveredAt = &now
	if !q.Delivered() {
		t.Fatal("a stamped message is delivered")
	}
}
