package campaign

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/lead"
	wo "vozko/domain/whatsapp_outreach"
)

func TestAReplyReachesAnOptedOutLeadOnlyAfterTheyWroteAgain(t *testing.T) {
	optedOut := time.Date(2026, 10, 20, 16, 33, 2, 0, time.UTC)
	contact := &lead.Lead{ID: "l1", Number: "5511999999999", OptedOutAt: &optedOut}
	before, after := optedOut.Add(-time.Second), optedOut.Add(time.Minute)

	if err := ReplyRefusal(contact, &after); err != nil {
		t.Fatalf("a message after the opt-out may be answered: %v", err)
	}
	for name, last := range map[string]*time.Time{"the opt-out message": &before, "no message": nil} {
		if err := ReplyRefusal(contact, last); !errors.Is(err, wo.ErrLeadOptedOut) {
			t.Fatalf("%s must stay refused, got %v", name, err)
		}
	}
	if err := RecipientRefusal(contact); !errors.Is(err, wo.ErrLeadOptedOut) {
		t.Fatalf("messages we start stay refused, got %v", err)
	}
}

func TestABlockedLeadGetsNoReplyEvenAfterWriting(t *testing.T) {
	now := time.Now()
	contact := &lead.Lead{ID: "l1", Number: "5511999999999", Blocked: true}
	if err := ReplyRefusal(contact, &now); !errors.Is(err, wo.ErrLeadBlocked) {
		t.Fatalf("got %v", err)
	}
	if err := ContactReplyRefusal(contact, &now); !errors.Is(err, wo.ErrLeadBlocked) {
		t.Fatalf("got %v", err)
	}
}
