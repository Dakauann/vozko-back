package emailinbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/emailtoken"
)

var campaignSentAt = time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)

func (w *world) campaignOriginal() {
	sent := w.sent.byID[campaignMsg]
	sent.ProviderMessageID = "ses-out-1"
	sent.SentAt = campaignSentAt
	sent.Status = emailsending.MessageDelivered
	w.originals.byMessage[campaignMsg] = em.SentOriginal{
		Subject: "Oferta de outubro", Text: "Olá Ana, confira a oferta.", SentBy: conversation.SentByCampaign("camp-1"),
		CampaignID: "camp-1", CampaignName: "Outubro",
	}
}

func TestAReplyToACampaignRecordsTheOriginalBeforeTheReply(t *testing.T) {
	w := newWorld(t)
	w.campaignOriginal()
	recipient := w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), recipient); err != nil {
		t.Fatal(err)
	}
	conv := w.onlyConversation()
	if len(w.history.records) != 2 {
		t.Fatalf("records = %d, want the original then the reply", len(w.history.records))
	}
	original, answer := w.history.records[0], w.history.records[1]
	if original.EntryID != conv.ID || original.ProviderMessageID != "ses-out-1" || original.Text != "Olá Ana, confira a oferta." ||
		original.SentBy.Kind() != conversation.SenderCampaign || original.MessageType != conversation.MessageTypeOperator ||
		!original.Timestamp.Equal(campaignSentAt) || original.DeliveryStatus != conversation.DeliveryStatusDelivered ||
		original.To != "ana@cliente.com" || original.From != "vendas@loja-a.com.br" {
		t.Fatalf("original = %+v", original)
	}
	meta := metadataOf(t, original)
	if meta[em.MetadataSubject] != "Oferta de outubro" || meta[em.MetadataCampaignID] != "camp-1" || meta[em.MetadataCampaignName] != "Outubro" {
		t.Fatalf("original metadata = %v", meta)
	}
	if !answer.SentBy.IsContact() || answer.ProviderMessageID != "m1@cliente.com" {
		t.Fatalf("reply = %+v", answer)
	}

	if err := w.receive(reply("ana@cliente.com", "m2@cliente.com"), recipient); err != nil {
		t.Fatal(err)
	}
	if len(w.history.records) != 3 || w.history.records[2].ProviderMessageID != "m2@cliente.com" {
		t.Fatalf("records after a second reply = %d, the original is recorded once", len(w.history.records))
	}
	if w.originals.calls != 1 {
		t.Fatalf("the original was rendered %d times, want only while it was missing", w.originals.calls)
	}
}

func TestAReplyFromAnotherAddressGetsTheOriginalSentToTheRecipient(t *testing.T) {
	w := newWorld(t)
	w.campaignOriginal()
	if err := w.receive(reply("colega@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	conv := w.onlyConversation()
	contact, err := w.contacts.FindByID(context.Background(), conv.ContactID)
	if err != nil || contact.Address != emailaddress.Address("colega@cliente.com") {
		t.Fatalf("conversation contact = %+v %v", contact, err)
	}
	if len(w.history.records) != 2 || w.history.records[0].To != "ana@cliente.com" || w.history.records[0].EntryID != conv.ID {
		t.Fatalf("records = %+v", w.history.records)
	}
}

func TestAnOriginalThatCannotBeRecalledLeavesOnlyTheReply(t *testing.T) {
	w := newWorld(t)
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	if len(w.history.records) != 1 || !w.history.records[0].SentBy.IsContact() || w.originals.calls != 1 {
		t.Fatalf("records = %d calls = %d", len(w.history.records), w.originals.calls)
	}
}

func TestARecallFailureRetriesTheNotificationBeforeAnyCharge(t *testing.T) {
	w := newWorld(t)
	w.originals.err = errors.New("db down")
	err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeReply, campaignMsg, replyA))
	if err == nil {
		t.Fatal("a failed recall must ask for a retry")
	}
	if len(w.history.records) != 0 || len(w.receipts.calls) != 0 {
		t.Fatalf("records = %d charges = %d before the retry", len(w.history.records), len(w.receipts.calls))
	}
}

func TestRepliesWithoutASentMessageRecallNothing(t *testing.T) {
	w := newWorld(t)
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), emailaddress.Address("vendas@"+replyA)); err != nil {
		t.Fatal(err)
	}
	if w.originals.calls != 0 || len(w.history.records) != 1 {
		t.Fatalf("calls = %d records = %d", w.originals.calls, len(w.history.records))
	}
}
