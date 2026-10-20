package messageaction

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
)

func failedTelegramText(id string) *conversation.Message {
	m := ownTelegramText(id, actionNow.Add(-time.Minute))
	m.DeliveryStatus = conversation.DeliveryStatusFailed
	replyTo := "m0"
	m.ReplyToMessageID = &replyTo
	return m
}

func TestResendRunsTheNormalSendWithTheOriginalContent(t *testing.T) {
	f := newActionFixture(t, failedTelegramText("m1"))

	sent, err := f.uc.Resend.Execute(context.Background(), bruno, target("m1"), "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if sent.ID != "msg-new" {
		t.Fatalf("answered %s, want the new message", sent.ID)
	}
	if len(f.send.inputs) != 1 {
		t.Fatalf("send calls = %d", len(f.send.inputs))
	}
	in := f.send.inputs[0]
	if in.EntryID != "entry-1" || in.EntryType != "telegram" || in.WorkspaceID != "ws-1" || in.SenderUserID != "user-bruno" ||
		in.Text != "oi" || in.ReplyToMessageID != "m0" || in.RequestID != "req-1" || in.Signed {
		t.Fatalf("send input = %+v", in)
	}
	if len(f.store.saved) != 1 || f.store.saved[0].ResentAs() != "msg-new" || len(f.announcer.updated) != 1 || f.announcer.updated[0] != "m1" {
		t.Fatalf("the failed bubble was not folded into the new one: saved %d announced %v", len(f.store.saved), f.announcer.updated)
	}
}

func TestOnlyAFailedOperatorMessageIsResent(t *testing.T) {
	f := newActionFixture(t, ownTelegramText("m1", actionNow), customerTelegramText("m2"))

	for _, id := range []string{"m1", "m2"} {
		_, err := f.uc.Resend.Execute(context.Background(), ana, target(id), "req-"+id)
		requireError(t, err, conversation.ErrResendNotAllowed)
	}
	if len(f.send.inputs) != 0 {
		t.Fatal("a message that did not fail was sent again")
	}
	f.requireNothingChanged(t)
}

func TestAFailedTemplateIsRefusedSoAStaleClientCannotSendItAsFreeText(t *testing.T) {
	template := failedTelegramText("m1")
	template.MessageType = conversation.MessageTypeTemplate
	f := newActionFixture(t, template)

	_, err := f.uc.Resend.Execute(context.Background(), ana, target("m1"), "req-1")
	requireError(t, err, conversation.ErrResendNotAllowed)
	if len(f.send.inputs) != 0 {
		t.Fatal("a failed template reached the operator send as free text")
	}
	f.requireNothingChanged(t)
}

func TestARepeatedResendAnswersWithTheMessageItAlreadyProduced(t *testing.T) {
	resent := failedTelegramText("m1")
	if err := resent.MarkResentAs("msg-new"); err != nil {
		t.Fatal(err)
	}
	replacement := ownTelegramText("msg-new", actionNow)
	replacement.ClientRequestID = "req-1"
	f := newActionFixture(t, resent, replacement)

	got, err := f.uc.Resend.Execute(context.Background(), ana, target("m1"), "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "msg-new" || len(f.send.inputs) != 0 {
		t.Fatalf("answered %s after %d sends, want the earlier replacement and no new send", got.ID, len(f.send.inputs))
	}

	_, err = f.uc.Resend.Execute(context.Background(), ana, target("m1"), "req-2")
	requireError(t, err, conversation.ErrResendNotAllowed)
}

func TestAResendThatFailsToDeliverLeavesTheFailedMessageAsItWas(t *testing.T) {
	f := newActionFixture(t, failedTelegramText("m1"))
	f.send.err = conversation.ErrOutboundWindowClosed

	_, err := f.uc.Resend.Execute(context.Background(), ana, target("m1"), "req-1")
	requireError(t, err, conversation.ErrResendDeliveryFailed)
	if !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Fatalf("err = %v, want the send reason kept", err)
	}
	f.requireNothingChanged(t)
}

func TestAResendStillInFlightIsNotAFailure(t *testing.T) {
	f := newActionFixture(t, failedTelegramText("m1"))
	f.send.err = conversation.ErrSendRequestInFlight

	_, err := f.uc.Resend.Execute(context.Background(), ana, target("m1"), "req-1")
	requireError(t, err, conversation.ErrSendRequestInFlight)
	if errors.Is(err, conversation.ErrResendDeliveryFailed) {
		t.Fatal("a request still being sent must not be reported as a failed delivery")
	}
}

func TestAResendNeedsARequestID(t *testing.T) {
	f := newActionFixture(t, failedTelegramText("m1"))

	_, err := f.uc.Resend.Execute(context.Background(), ana, target("m1"), " ")
	requireError(t, err, conversation.ErrClientRequestIDRequired)
	if len(f.send.inputs) != 0 {
		t.Fatal("a resend without a request id could be replayed into a second send")
	}
}

func TestResendFollowsTheSendAuthorization(t *testing.T) {
	f := newActionFixture(t, failedTelegramText("m1"))
	f.access.permission = false

	_, err := f.uc.Resend.Execute(context.Background(), ana, target("m1"), "req-1")
	requireError(t, err, conversation.ErrUnauthorized)
	if len(f.send.inputs) != 0 {
		t.Fatal("an unauthorized resend reached the send")
	}
}
