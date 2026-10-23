package emailinbox

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/emailtoken"
)

func (w *world) receiveNotice(msg em.InboundMessage, verdict emailsending.InboundVerdict, recipients ...emailaddress.Address) error {
	w.parser.msg = msg
	return w.inbound.HandleInbound(context.Background(), emailsending.InboundNotice{
		Recipients: recipients, ObjectKey: objectKey, MessageID: "ses-in-1", Verdict: verdict,
		ReceivedAt: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
	})
}

func TestARejectedVerdictDeletesTheRawCopyAndStops(t *testing.T) {
	for _, verdict := range []emailsending.InboundVerdict{
		{Spam: emailsending.VerdictFail, Virus: emailsending.VerdictPass},
		{Spam: emailsending.VerdictPass, Virus: emailsending.VerdictFail},
	} {
		w := newWorld(t)
		w.senderIP.ip, w.senderIP.ok = netip.MustParseAddr("203.0.113.7"), true
		w.raw.err = errors.New("the raw copy is never fetched")
		if err := w.receiveNotice(reply("ana@cliente.com", "m1@cliente.com"), verdict, w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(w.raw.deleted, []string{objectKey}) {
			t.Fatalf("deleted = %v", w.raw.deleted)
		}
		if len(w.convs.byID) != 0 || len(w.history.records) != 0 || len(w.receipts.calls) != 0 || len(w.guard.senders) != 0 {
			t.Fatalf("a rejected message reached the inbox: convs %d records %d charges %d counted %d",
				len(w.convs.byID), len(w.history.records), len(w.receipts.calls), len(w.guard.senders))
		}
	}
}

func TestARejectedMessageWhoseCopyCannotBeDeletedIsRetried(t *testing.T) {
	w := newWorld(t)
	w.raw.deleteErr = errors.New("access denied")
	err := w.receiveNotice(reply("ana@cliente.com", "m1@cliente.com"), emailsending.InboundVerdict{Spam: emailsending.VerdictFail}, w.tagged(emailtoken.PurposeReply, campaignMsg, replyA))
	if err == nil {
		t.Fatal("a failed delete must surface so the notification is retried")
	}
}

func TestEveryAcceptedMessageCountsItsSender(t *testing.T) {
	w := newWorld(t)
	ip := netip.MustParseAddr("203.0.113.7")
	w.senderIP.ip, w.senderIP.ok = ip, true
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), "ninguem@dominio-desconhecido.com"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.guard.senders, []netip.Addr{ip}) || string(w.senderIP.got[0]) != "raw" {
		t.Fatalf("counted = %v read = %q", w.guard.senders, w.senderIP.got)
	}
	if len(w.receipts.calls) != 0 {
		t.Fatal("mail for an unknown domain is never charged to anyone")
	}
}

func TestAnUnreadableSenderIsNeverCounted(t *testing.T) {
	w := newWorld(t)
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	if len(w.guard.senders) != 0 || len(w.history.records) != 1 {
		t.Fatalf("counted = %v records = %d", w.guard.senders, len(w.history.records))
	}
}

func TestAReplyIsChargedBeforeItIsRecorded(t *testing.T) {
	w := newWorld(t)
	var order []string
	w.receipts.order = &order
	w.raw.objects[objectKey] = make([]byte, 300<<10)
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	if len(w.receipts.calls) != 1 {
		t.Fatalf("charges = %+v", w.receipts.calls)
	}
	call := w.receipts.calls[0]
	if call.workspaceID != wsA || call.key != "ses-in-1" || call.size != 300<<10 {
		t.Fatalf("charge = %+v", call)
	}
	if len(order) != 1 || len(w.history.records) != 1 {
		t.Fatalf("order = %v records = %d", order, len(w.history.records))
	}
	if len(w.guard.paused) != 0 {
		t.Fatal("a positive wallet keeps receiving")
	}
}

func TestAFailedChargeRecordsNothing(t *testing.T) {
	w := newWorld(t)
	w.receipts.err = errors.New("price unavailable")
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err == nil {
		t.Fatal("the charge failure must surface so the notification is retried")
	}
	if len(w.history.records) != 0 || len(w.dispatcher.inputs) != 0 {
		t.Fatalf("records = %d dispatch = %d", len(w.history.records), len(w.dispatcher.inputs))
	}
}

func TestANegativeWalletPausesRepliesAndStillRecordsThePaidMessage(t *testing.T) {
	for _, pauseErr := range []error{nil, errors.New("ses down")} {
		w := newWorld(t)
		w.receipts.balanceAfter = -1
		w.guard.pauseErr = pauseErr
		if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(w.guard.paused, []string{wsA}) {
			t.Fatalf("paused = %v", w.guard.paused)
		}
		if len(w.history.records) != 1 || w.history.records[0].MessageType != conversation.MessageTypeUserMessage {
			t.Fatalf("records = %+v", w.history.records)
		}
	}
}

func TestAZeroBalanceKeepsReceiving(t *testing.T) {
	w := newWorld(t)
	w.receipts.balanceAfter = 0
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	if len(w.guard.paused) != 0 {
		t.Fatalf("paused = %v", w.guard.paused)
	}
}

func TestAnUnsubscribeByEmailIsNeverCharged(t *testing.T) {
	w := newWorld(t)
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeUnsubscribe, campaignMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	if len(w.unsub.calls) != 1 || len(w.receipts.calls) != 0 {
		t.Fatalf("unsubscribes = %v charges = %v", w.unsub.calls, w.receipts.calls)
	}
}

func TestAttachmentsShareOneBudgetPerEmail(t *testing.T) {
	w := newWorld(t)
	msg := reply("ana@cliente.com", "m1@cliente.com")
	big := make([]byte, 15<<20)
	msg.Attachments = []em.Attachment{
		{FileName: "a.pdf", ContentType: "application/pdf", Data: big, Size: int64(len(big))},
		{FileName: "b.pdf", ContentType: "application/pdf", Data: big, Size: int64(len(big))},
		{FileName: "c.png", ContentType: "image/png", Data: []byte("png"), Size: 3},
	}
	if err := w.receive(msg, w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	if len(w.history.records) != 4 || len(w.media.inputs) != 2 {
		t.Fatalf("records = %d stored = %d", len(w.history.records), len(w.media.inputs))
	}
	dropped := w.history.records[2]
	if dropped.MessageType != conversation.MessageTypeUnsupported || dropped.Text != em.UnavailableAttachmentText ||
		metadataOf(t, dropped)[em.MetadataFileName] != "b.pdf" || metadataOf(t, dropped)[em.MetadataAttachmentUnavailable] != true {
		t.Fatalf("over budget attachment = %+v", dropped)
	}
	if w.media.inputs[1].OriginalFilename != "c.png" {
		t.Fatalf("stored = %+v", w.media.inputs[1])
	}
}
