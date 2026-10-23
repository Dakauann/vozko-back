package carriermessaging_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
)

func TestDeliverRefusesAChannelThatIsNotLaunched(t *testing.T) {
	h := newHarness(t)
	h.svc.launched = carriermessaging.Channel.Launched
	if _, err := h.svc.Deliver(context.Background(), marketing(rcs.Content{Text: "Oi"})); !errors.Is(err, carriermessaging.ErrChannelNotLaunched) {
		t.Fatalf("RCS deliver = %v", err)
	}
	if len(h.provider.sends) != 0 || len(h.messages.all()) != 0 || len(h.ledger.debits) != 0 {
		t.Fatal("a refused channel sends, stores and charges nothing")
	}
	sms := DeliverRequest{WorkspaceID: "ws-1", Channel: carriermessaging.ChannelSMS, SenderID: "route-1", Phone: "+55 11 99999-0000", RunKind: carriermessaging.RunOperator, Text: "Oi"}
	if _, err := h.svc.Deliver(context.Background(), sms); (err == nil) != carriermessaging.ChannelSMS.Launched() {
		t.Fatalf("SMS deliver = %v while SMS launched is %v", err, carriermessaging.ChannelSMS.Launched())
	}
}

func TestNewServiceUsesTheRealLaunchRule(t *testing.T) {
	h := newHarness(t)
	svc, err := NewService(h.svc.Deps)
	if err != nil {
		t.Fatal(err)
	}
	if svc.launched(carriermessaging.ChannelRCS) != carriermessaging.ChannelRCS.Launched() || svc.launched(carriermessaging.ChannelSMS) != carriermessaging.ChannelSMS.Launched() {
		t.Fatal("production services follow carriermessaging.Channel.Launched")
	}
}
