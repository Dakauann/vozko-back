package emailwiring_test

import (
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	"vozko/domain/emailconsent"
	es "vozko/domain/emailsending"
	"vozko/domain/emailtoken"
	"vozko/domain/workspace/workspace_pricing"
	"vozko/infra/database/schema"
	emailcampaign_repository "vozko/infra/repositories/emailcampaign"
	"vozko/infra/ses/sesfake"
	balance_usecase "vozko/usecases/balance"
)

var customerIP = netip.MustParseAddr("203.0.113.7")

type replyPrices struct {
	perMessage int64
	perChunk   int64
}

func (w *world) replyPrices(ws *tenant) replyPrices {
	w.t.Helper()
	message, err := w.pricer.PriceEmail(ws.ID, workspace_pricing.EmailReceivePerMessage)
	w.must(err)
	chunk, err := w.pricer.PriceEmail(ws.ID, workspace_pricing.EmailReceivePerChunk)
	w.must(err)
	return replyPrices{perMessage: message.PriceMicros, perChunk: chunk.PriceMicros}
}

func (p replyPrices) of(rawBytes int) int64 {
	return p.perMessage + p.perChunk*es.BillableChunks(int64(rawBytes))
}

func (w *world) receive(in sesfake.Inbound) sesfake.Received {
	w.t.Helper()
	received := w.ses.Receive(in)
	if !received.Accepted {
		w.t.Fatalf("SES refused %v from %s during the SMTP conversation", in.To, in.SenderIP)
	}
	w.postSNS(inboundTopic, received.Notification)
	return received
}

func (w *world) receiveCharge(ws *tenant, sesMessageID string) []schema.BalanceTransaction {
	return w.ledger(ws, "debit", balance_usecase.EmailReceiveReference(sesMessageID))
}

func (w *world) replyFrom(lead schema.Lead, to emailaddress.Address, text string, size int) sesfake.Inbound {
	return sesfake.Inbound{
		From: lead.Name + " <" + string(lead.Email) + ">", To: []emailaddress.Address{to}, Subject: "Re: Pedido confirmado",
		Text: text, SizeBytes: size, SenderIP: customerIP,
	}
}

func inbound(rows []transcriptRow) []transcriptRow {
	var out []transcriptRow
	for _, r := range rows {
		if r.SenderKind == string(conversation.SenderContact) {
			out = append(out, r)
		}
	}
	return out
}

func TestEmailE2E_RepliesAreRoutedChargedAndFiltered(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Respostas", roomyWallet)
	prices := w.replyPrices(ws)
	people := w.seedLeads(ws, 1, "resposta")
	ana := people[0]
	res, err := w.sendFromWorkflow(ws, ana, "pedido:"+ana.ID)
	w.must(err)
	replyTo := w.onlySentTo(emailaddress.Address(ana.Email)).Message.ReplyTo[0]

	small := w.receive(w.replyFrom(ana, replyTo, "Obrigada, chegou certinho!", 0))
	rows := inbound(w.transcript(res.ConversationID))
	if len(rows) != 1 || rows[0].Text != "Obrigada, chegou certinho!" {
		t.Fatalf("inbound rows = %+v, want the reply in the conversation of the sent email", rows)
	}
	if w.conversationsOf(ws) != 1 {
		t.Fatalf("conversations = %d, the reply opened another", w.conversationsOf(ws))
	}
	if charge := w.receiveCharge(ws, small.MessageID); len(charge) != 1 || charge[0].Amount != prices.of(small.RawBytes) || charge[0].Amount != prices.perMessage {
		t.Fatalf("small reply charged %v, want one line of %d", amounts(charge), prices.perMessage)
	}
	if w.automation.count() != 1 {
		t.Fatalf("automation ran %d times", w.automation.count())
	}

	large := w.receive(w.replyFrom(ana, replyTo, "Segue o comprovante.", 600*1024))
	if chunks := es.BillableChunks(int64(large.RawBytes)); chunks != 2 {
		t.Fatalf("a %d byte reply has %d complete chunks, want 2", large.RawBytes, chunks)
	}
	if charge := w.receiveCharge(ws, large.MessageID); len(charge) != 1 || charge[0].Amount != prices.perMessage+2*prices.perChunk {
		t.Fatalf("large reply charged %v, want %d", amounts(charge), prices.perMessage+2*prices.perChunk)
	}
	w.postSNS(inboundTopic, large.Notification)
	if charge := w.receiveCharge(ws, large.MessageID); len(charge) != 1 {
		t.Fatalf("a redelivered reply was charged %d times", len(charge))
	}
	if rows := inbound(w.transcript(res.ConversationID)); len(rows) != 2 {
		t.Fatalf("inbound rows = %d after a redelivery, want 2", len(rows))
	}

	for _, verdict := range []sesfake.Inbound{
		{Spam: es.VerdictFail},
		{Virus: es.VerdictFail},
	} {
		in := w.replyFrom(ana, replyTo, "Compre agora!!!", 0)
		in.Spam, in.Virus = verdict.Spam, verdict.Virus
		rejected := w.receive(in)
		if w.ses.HasRaw(rejected.ObjectKey) || !slices.Contains(w.ses.DeletedRaw(), rejected.ObjectKey) {
			t.Fatalf("the raw copy of a rejected reply %s was kept", rejected.ObjectKey)
		}
		if charge := w.receiveCharge(ws, rejected.MessageID); len(charge) != 0 {
			t.Fatalf("a rejected reply was charged %v", amounts(charge))
		}
	}
	if rows := inbound(w.transcript(res.ConversationID)); len(rows) != 2 {
		t.Fatalf("inbound rows = %d, rejected replies must not be recorded", len(rows))
	}

	other := w.sendingTenant("Outra Loja", roomyWallet)
	tagged, _, _ := strings.Cut(string(replyTo), "@")
	forged := w.replyFrom(ana, emailaddress.Address(tagged+"@"+other.Domain.ReplyDomain), "token trocado", 0)
	crossed := w.receive(forged)
	if charge := w.receiveCharge(other, crossed.MessageID); len(charge) != 0 || w.conversationsOf(other) != 0 {
		t.Fatal("a token of one workspace arriving on another workspace's reply domain was delivered")
	}
	if rows := inbound(w.transcript(res.ConversationID)); len(rows) != 2 {
		t.Fatalf("the crossed token reached the original conversation: %d rows", len(rows))
	}

	want := ws.Start - w.unit(ws) - prices.perMessage - (prices.perMessage + 2*prices.perChunk)
	if got := w.wallet(ws); got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertLedger(ws)
}

func TestEmailE2E_TheReplyThatCrossesZeroIsKeptAndPausesOnlyReplyAt(t *testing.T) {
	w := newWorld(t)
	unit := w.defaultUnit()
	ws := w.sendingTenant("Loja Devendo", unit+100)
	prices := w.replyPrices(ws)
	ana := w.seedLeads(ws, 1, "devedora")[0]
	res, err := w.sendFromWorkflow(ws, ana, "pedido:"+ana.ID)
	w.must(err)
	replyTo := w.onlySentTo(emailaddress.Address(ana.Email)).Message.ReplyTo[0]
	replyDomain := ws.Domain.ReplyDomain
	if got := w.wallet(ws); got != 100 {
		t.Fatalf("wallet after the send = %d", got)
	}

	crossing := w.receive(w.replyFrom(ana, replyTo, "Pode cancelar?", 0))

	if rows := inbound(w.transcript(res.ConversationID)); len(rows) != 1 {
		t.Fatalf("inbound rows = %d, the reply SES already accepted must be recorded", len(rows))
	}
	if charge := w.receiveCharge(ws, crossing.MessageID); len(charge) != 1 || charge[0].Amount != prices.perMessage {
		t.Fatalf("crossing reply charged %v", amounts(charge))
	}
	if got := w.wallet(ws); got != 100-prices.perMessage || got >= 0 {
		t.Fatalf("wallet = %d, want %d", got, 100-prices.perMessage)
	}
	if got := w.ses.Receiving(replyDomain); !slices.Equal(got, []string{es.UnsubscribeRecipient(replyDomain)}) {
		t.Fatalf("receipt conditions = %v, want only unsubscribe@ while the wallet is negative", got)
	}
	domain, err := w.domains.Get(w.ctx, ws.ID, ws.Domain.ID)
	w.must(err)
	if !domain.RepliesPaused() {
		t.Fatal("the domain is not stamped replies_paused_at")
	}
	episode := "email_replies_paused:" + ws.ID
	if w.alerts.publishedTimes(episode) != 1 {
		t.Fatalf("replies paused episode published %d times", w.alerts.publishedTimes(episode))
	}
	w.must(w.module.ReplySweep.Execute(w.ctx))
	if got := w.ses.Receiving(replyDomain); len(got) != 1 || w.alerts.publishedTimes(episode) != 1 {
		t.Fatalf("a sweep while still negative changed the route (%v) or alerted again", got)
	}
	if w.ses.Receive(w.replyFrom(ana, replyTo, "Ainda aí?", 0)).Accepted {
		t.Fatal("SES accepted a reply while reply@ is paused")
	}
	unsubscribe, err := w.codec.TaggedAddress(emailtoken.PurposeUnsubscribe, w.message(w.onlySentTo(emailaddress.Address(ana.Email)).ProviderMessageID).ID, replyDomain)
	w.must(err)
	owed := w.wallet(ws)
	w.receive(w.replyFrom(ana, unsubscribe, "unsubscribe", 0))
	sub, err := emailcampaign_repository.NewSubscriptionRepository(w.db).Get(w.ctx, ws.ID, emailaddress.Address(ana.Email))
	w.must(err)
	if sub.Status != emailconsent.StatusUnsubscribed || sub.UnsubscribeSource != emailconsent.UnsubscribeMailto || w.wallet(ws) != owed {
		t.Fatalf("an unsubscribe by email while paused = %+v, wallet %d (was %d): it is honored and never charged", sub, w.wallet(ws), owed)
	}
	w.assertLedgerOwingOnlyForReplies(ws)

	w.topUp(ws, 10_000)
	w.must(w.module.ReplySweep.Execute(w.ctx))

	if got := w.ses.Receiving(replyDomain); !slices.Equal(got, es.InboundRecipients(replyDomain)) {
		t.Fatalf("receipt conditions after the top up = %v, want reply@ back", got)
	}
	domain, err = w.domains.Get(w.ctx, ws.ID, ws.Domain.ID)
	w.must(err)
	if domain.RepliesPaused() || w.alerts.resolvedTimes(episode) != 1 {
		t.Fatalf("paused = %v resolved %d times", domain.RepliesPaused(), w.alerts.resolvedTimes(episode))
	}
	back := w.receive(w.replyFrom(ana, replyTo, "Obrigada!", 0))
	if charge := w.receiveCharge(ws, back.MessageID); len(charge) != 1 {
		t.Fatalf("the reply after the resume charged %d lines", len(charge))
	}
	if rows := inbound(w.transcript(res.ConversationID)); len(rows) != 2 {
		t.Fatalf("inbound rows = %d", len(rows))
	}
	w.assertLedgerOwingOnlyForReplies(ws)
}

func TestEmailE2E_ThreeHundredMessagesInTenMinutesBlockTheSendingIP(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Flood", roomyWallet)
	prices := w.replyPrices(ws)
	flooder := netip.MustParseAddr("198.51.100.9")
	forged := sesfake.Inbound{From: "Robo <robo@spam.example>", To: []emailaddress.Address{emailaddress.Address("reply+naoexiste@" + ws.Domain.ReplyDomain)}, Subject: "Oferta", Text: "spam", SenderIP: flooder}
	for i := 0; i < es.InboundFloodThreshold-1; i++ {
		w.receive(forged)
	}
	if _, blocked := w.ses.Blocked(flooder); blocked {
		t.Fatalf("blocked after %d messages", es.InboundFloodThreshold-1)
	}
	before := time.Now()
	w.receive(forged)
	until, blocked := w.ses.Blocked(flooder)
	if !blocked || until.Before(before.Add(es.InboundBlockDuration-time.Minute)) || until.After(time.Now().Add(es.InboundBlockDuration)) {
		t.Fatalf("blocked = %v until %v, want a block of %s at message %d", blocked, until, es.InboundBlockDuration, es.InboundFloodThreshold)
	}
	if !w.ops.raised("Remetente de e-mail bloqueado") {
		t.Fatal("no ops alert for the block")
	}
	if w.ses.Receive(forged).Accepted {
		t.Fatal("SES accepted mail from a blocked IP")
	}
	if !w.ses.Receive(w.replyFrom(w.seedLeads(ws, 1, "inocente")[0], emailaddress.Address("reply+x@"+ws.Domain.ReplyDomain), "oi", 0)).Accepted {
		t.Fatal("mail from another IP was refused")
	}
	charges := w.ledger(ws, "debit", "email_receive:%")
	if len(charges) != es.InboundFloodThreshold || sumOf(charges) != int64(es.InboundFloodThreshold)*prices.perMessage {
		t.Fatalf("receive charges = %d totalling %d: forged mail that SES accepted is charged once per message, as built", len(charges), sumOf(charges))
	}
	if got, want := w.wallet(ws), ws.Start-int64(es.InboundFloodThreshold)*prices.perMessage; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertLedger(ws)
}
