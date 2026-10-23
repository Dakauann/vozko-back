package emailwiring_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailconsent"
	es "vozko/domain/emailsending"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	conversation_repository "vozko/infra/repositories/conversation"
	emailcampaign_repository "vozko/infra/repositories/emailcampaign"
	"vozko/infra/ses"
	"vozko/infra/ses/sesfake"
	conversation_usecase "vozko/usecases/conversation"
	esuc "vozko/usecases/emailsending"
	"vozko/usecases/transactionalemail"
)

func (w *world) sendFromWorkflow(ws *tenant, lead schema.Lead, key string) (transactionalemail.Result, error) {
	w.t.Helper()
	return w.relay.Send(w.ctx, transactionalemail.Request{
		WorkspaceID: ws.ID, LeadID: lead.ID, Subject: "Pedido confirmado, {{first_name}}", Body: transactionalemail.Body{Text: "Recebemos seu pedido."},
		IdempotencyKey: key, Origin: transactionalemail.OriginWorkflow, SentBy: conversation.SentByWorkflow("wf-pedidos"),
	})
}

func (w *world) message(providerID string) es.Message {
	w.t.Helper()
	m, err := w.messages.GetByProviderID(w.ctx, providerID)
	w.must(err)
	return *m
}

func (w *world) eventRows(messageID string) int64 {
	w.t.Helper()
	var n int64
	w.must(w.db.Raw("SELECT count(*) FROM email_message_events WHERE message_id = ?", messageID).Scan(&n).Error)
	return n
}

func (w *world) transcriptStatus(t *testing.T, conversationID, providerID string) string {
	t.Helper()
	for _, row := range w.transcript(conversationID) {
		if row.ExternalMessageID == providerID {
			return row.DeliveryStatus
		}
	}
	t.Fatalf("conversation %s has no row for %s", conversationID, providerID)
	return ""
}

func TestEmailE2E_ProviderEventsReachMessagesSuppressionsConsentAndTheConversation(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Eventos", roomyWallet)
	unit := w.unit(ws)
	people := w.seedLeads(ws, 3, "evento")
	ana, bia, caio := people[0], people[1], people[2]
	sent := map[string]sesfake.Sent{}
	conversations := map[string]string{}
	for _, lead := range people {
		res, err := w.sendFromWorkflow(ws, lead, "pedido:"+lead.ID)
		w.must(err)
		if res.ConversationID == "" || res.Repeated {
			t.Fatalf("send to %s = %+v, want a recorded first send", lead.Email, res)
		}
		sent[lead.ID] = w.onlySentTo(emailaddress.Address(lead.Email))
		conversations[lead.ID] = res.ConversationID
		if status := w.transcriptStatus(t, res.ConversationID, sent[lead.ID].ProviderMessageID); status != string(conversation.DeliveryStatusNone) {
			t.Fatalf("a fresh transcript row is %q", status)
		}
	}

	deliveredAt := time.Now().UTC()
	delivery := sesfake.Delivery(sent[ana.ID], deliveredAt)
	snsID := uuid.NewString()
	w.postSNSAs(snsID, eventsTopic, delivery)
	delivered := w.message(sent[ana.ID].ProviderMessageID)
	if delivered.Status != es.MessageDelivered || delivered.DeliveredAt == nil {
		t.Fatalf("ana's message = %s", delivered.Status)
	}
	if status := w.transcriptStatus(t, conversations[ana.ID], sent[ana.ID].ProviderMessageID); status != string(conversation.DeliveryStatusDelivered) {
		t.Fatalf("ana's conversation row = %q, want delivered", status)
	}
	w.postSNSAs(snsID, eventsTopic, delivery)
	w.postSNS(eventsTopic, delivery)
	if rows := w.eventRows(delivered.ID); rows != 1 {
		t.Fatalf("an SNS redelivery and a provider replay stored %d events, want one", rows)
	}
	if updates := w.announcer.count(conversations[ana.ID]); updates != 1 {
		t.Fatalf("the conversation row was announced %d times, want once", updates)
	}

	sender := conversation_usecase.NewMessageSenderService(conversation_repository.NewRepository(w.db), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	sender.SetChannelAdapters(conversation.NewAdapterRegistry(w.inbox.Adapter))
	reply, err := sender.SendTextMessage(conversations[ana.ID], string(shared.EntryTypeEmail), "Que bom que chegou!", conversation.SendOrigin{SenderUserID: ws.Actor.UserID})
	w.must(err)
	operatorReplies := w.ses.SentTo(emailaddress.Address(ana.Email))
	if len(operatorReplies) != 2 || reply.ExternalMessageID == nil || *reply.ExternalMessageID != operatorReplies[1].ProviderMessageID {
		t.Fatalf("operator reply = %+v, provider sends %d", reply, len(operatorReplies))
	}
	w.providerEvent(sesfake.Delivery, operatorReplies[1])
	if status := w.transcriptStatus(t, conversations[ana.ID], operatorReplies[1].ProviderMessageID); status != string(conversation.DeliveryStatusDelivered) {
		t.Fatalf("the operator reply row = %q, want delivered", status)
	}

	w.providerEvent(sesfake.HardBounce, sent[bia.ID])
	bounced := w.message(sent[bia.ID].ProviderMessageID)
	if bounced.Status != es.MessageBounced || bounced.BounceKind != es.BounceHard {
		t.Fatalf("bia's message = %s %s", bounced.Status, bounced.BounceKind)
	}
	if status := w.transcriptStatus(t, conversations[bia.ID], sent[bia.ID].ProviderMessageID); status != string(conversation.DeliveryStatusFailed) {
		t.Fatalf("bia's conversation row = %q, want failed", status)
	}
	suppressed, err := w.suppressions.Suppressed(w.ctx, ws.ID, []emailaddress.Address{emailaddress.Address(bia.Email)})
	w.must(err)
	if suppressed[emailaddress.Address(bia.Email)] != es.SuppressHardBounce {
		t.Fatalf("suppressions = %v, want bia suppressed for the hard bounce", suppressed)
	}
	walletBefore := w.wallet(ws)
	_, err = w.sendFromWorkflow(ws, bia, "pedido-2:"+bia.ID)
	if code := transactionalemail.CodeOf(err); code != transactionalemail.CodeRecipientSuppressed {
		t.Fatalf("a later send to a bounced address = %v (%s)", err, code)
	}
	if len(w.ses.SentTo(emailaddress.Address(bia.Email))) != 1 || w.wallet(ws) != walletBefore {
		t.Fatalf("the refused send reached the provider or kept a charge (wallet %d, was %d)", w.wallet(ws), walletBefore)
	}

	w.providerEvent(sesfake.Complaint, sent[caio.ID])
	complained := w.message(sent[caio.ID].ProviderMessageID)
	if complained.ComplainedAt == nil || complained.UnsubscribedAt == nil {
		t.Fatalf("caio's message = %+v, want complained and unsubscribed", complained)
	}
	suppressed, err = w.suppressions.Suppressed(w.ctx, ws.ID, []emailaddress.Address{emailaddress.Address(caio.Email)})
	w.must(err)
	if suppressed[emailaddress.Address(caio.Email)] != es.SuppressComplaint {
		t.Fatalf("suppressions = %v, want caio suppressed for the complaint", suppressed)
	}
	sub, err := emailcampaign_repository.NewSubscriptionRepository(w.db).Get(w.ctx, ws.ID, emailaddress.Address(caio.Email))
	w.must(err)
	if sub.Status != emailconsent.StatusUnsubscribed || sub.UnsubscribeSource != emailconsent.UnsubscribeComplaint {
		t.Fatalf("subscription = %+v, want unsubscribed by the complaint", sub)
	}

	c := w.campaignToEveryone(ws)
	w.dispatch(c, 2, 0)
	counts := w.counts(c)
	if counts.Sent != 1 || counts.Skipped[emailcampaign.SkipSuppressed]+counts.Skipped[emailcampaign.SkipUnsubscribed] != 2 {
		t.Fatalf("later campaign counts = %+v, want ana sent and the bounced and complained addresses skipped", counts)
	}
	w.assertBatches(ws, c, 1)
	if len(w.ses.SentTo(emailaddress.Address(bia.Email))) != 1 || len(w.ses.SentTo(emailaddress.Address(caio.Email))) != 1 {
		t.Fatal("the later campaign reached a suppressed address")
	}
	if got, want := w.wallet(ws), ws.Start-4*unit-unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertSlots(c)
	w.assertLedger(ws)
}

func TestEmailE2E_UnknownProviderEventsAreDroppedWithoutAnError(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Desconhecida", roomyWallet)
	ghost := sesfake.Sent{ProviderMessageID: "0100" + uuid.NewString(), Message: es.OutboundMessage{TenantName: "ws-" + ws.ID, To: "ninguem@cliente.example"}}
	w.providerEvent(sesfake.HardBounce, ghost)
	suppressed, err := w.suppressions.Suppressed(w.ctx, ws.ID, []emailaddress.Address{"ninguem@cliente.example"})
	w.must(err)
	if len(suppressed) != 0 {
		t.Fatalf("an event for a message we never sent suppressed %v", suppressed)
	}
	if _, err := w.messages.GetByProviderID(w.ctx, ghost.ProviderMessageID); !errors.Is(err, es.ErrMessageNotFound) {
		t.Fatalf("lookup = %v", err)
	}
}

func TestEmailE2E_TheEventTopicValidationNoticeIsAcceptedQuietly(t *testing.T) {
	w := newWorld(t)
	notice := "Successfully validated SNS topic for Amazon SES event publishing."
	if err := esuc.HandleQueued(w.ctx, ses.Decoder{}, w.module.Events, esuc.QueuedNotification{MessageID: uuid.NewString(), TopicARN: eventsTopic, Message: notice}); err != nil {
		t.Fatalf("the notice must be handled, not dropped as undecodable: %v", err)
	}
	w.postSNS(eventsTopic, []byte(notice))
}

func TestEmailE2E_TheReceiptRuleSetupNotificationIsSettledWithoutTouchingStorage(t *testing.T) {
	w := newWorld(t)
	notice := `{"notificationType":"Received","receipt":{"timestamp":"2026-10-22T13:00:01.120Z","recipients":["recipient@example.com"],` +
		`"spamVerdict":{"status":"PASS"},"virusVerdict":{"status":"PASS"},"action":{"type":"S3","topicArn":"` + inboundTopic + `",` +
		`"bucketName":"vozko-inbound","objectKeyPrefix":"inbound/","objectKey":"inbound/AMAZON_SES_SETUP_NOTIFICATION"}},` +
		`"mail":{"timestamp":"2026-10-22T13:00:01.120Z","source":"amazon-ses-setup@amazonses.com","messageId":"AMAZON_SES_SETUP_NOTIFICATION","destination":["recipient@example.com"]}}`
	w.postSNS(inboundTopic, []byte(notice))
	if deleted := w.ses.DeletedRaw(); len(deleted) != 0 {
		t.Fatalf("the setup notification touched the bucket: %v", deleted)
	}
}
