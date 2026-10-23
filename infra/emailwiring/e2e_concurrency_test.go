package emailwiring_test

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
	"vozko/infra/database/schema"
	"vozko/usecases/transactionalemail"
)

type sendTally struct {
	mu         sync.Mutex
	sent       int
	short      int
	unexpected []error
}

func (s *sendTally) record(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case err == nil:
		s.sent++
	case transactionalemail.CodeOf(err) == transactionalemail.CodeBalanceInsufficient:
		s.short++
	default:
		s.unexpected = append(s.unexpected, err)
	}
}

func (w *world) assertEveryDebitIsBacked(ws *tenant) {
	w.t.Helper()
	credits := map[string]bool{}
	for _, row := range w.ledger(ws, "credit", "refund:%") {
		credits[strings.TrimPrefix(*row.ReferenceID, "refund:")] = true
	}
	batches := map[string]int64{}
	var rows []struct {
		CampaignID  string
		BatchNo     int
		TotalMicros int64
	}
	w.must(w.db.Raw("SELECT campaign_id, batch_no, total_micros FROM email_campaign_charge_batches WHERE charged_at IS NOT NULL").Scan(&rows).Error)
	for _, r := range rows {
		batches["email_campaign:"+r.CampaignID+":batch:"+strconv.Itoa(r.BatchNo)] = r.TotalMicros
	}
	for _, debit := range w.ledger(ws, "debit", "%") {
		ref := *debit.ReferenceID
		switch {
		case strings.HasPrefix(ref, "email_campaign:"):
			if total, ok := batches[ref]; !ok || total != debit.Amount {
				w.t.Fatalf("debit %s of %d has no charged batch of that amount", ref, debit.Amount)
			}
		case strings.HasPrefix(ref, "email_message:tx:"):
			if credits[ref] {
				continue
			}
			runID, _, _ := strings.Cut(strings.TrimPrefix(ref, "email_message:tx:"), "#")
			sent, err := w.runs.ListByRun(w.ctx, ws.ID, es.RunTriggered, runID)
			w.must(err)
			if len(sent) != 1 || sent[0].Status == es.MessageFailed {
				w.t.Fatalf("debit %s kept its charge without a sent message (%d rows)", ref, len(sent))
			}
		case strings.HasPrefix(ref, "email_receive:"):
		default:
			w.t.Fatalf("unexpected debit %s", ref)
		}
	}
}

func TestEmailE2E_ConcurrentCampaignWorkersAndOneToOneSendsNeverOverspendATightWallet(t *testing.T) {
	w := newWorld(t)
	unit := w.defaultUnit()
	ws := w.sendingTenant("Loja Concorrida", 1_200*unit)
	prices := w.replyPrices(ws)
	c := w.launch(ws, 1_500)
	direct := w.seedLeads(ws, 400, "direto")
	primer, err := w.sendFromWorkflow(ws, direct[0], "direto:primeiro")
	w.must(err)
	replyTo := w.onlySentTo(emailaddress.Address(direct[0].Email)).Message.ReplyTo[0]

	var wg sync.WaitGroup
	var campaignErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, campaignErr = w.queue.tryDrain(campaignTopic(c), campaignWorkers, 0)
	}()
	tally := &sendTally{}
	work := make(chan schema.Lead, len(direct))
	for _, lead := range direct[1:] {
		work <- lead
	}
	close(work)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for lead := range work {
				_, err := w.sendFromWorkflow(ws, lead, "direto:"+lead.ID)
				tally.record(err)
			}
		}()
	}
	const replies = 10
	for i := 0; i < replies; i++ {
		w.receive(w.replyFrom(direct[0], replyTo, "Mais uma dúvida", 0))
	}
	wg.Wait()

	if campaignErr != nil {
		t.Fatal(campaignErr)
	}
	if len(tally.unexpected) > 0 {
		t.Fatalf("%d one-to-one sends failed unexpectedly, first: %v", len(tally.unexpected), tally.unexpected[0])
	}
	if tally.sent+tally.short != len(direct)-1 {
		t.Fatalf("tally = %+v", tally)
	}
	counts := w.counts(c)
	campaignSent := int(counts.Sent)
	if got, want := len(w.ses.Sent()), campaignSent+tally.sent+1; got != want {
		t.Fatalf("provider sends = %d, want %d campaign + %d one-to-one + 1", got, campaignSent, tally.sent)
	}
	w.assertSlots(c)
	w.assertEveryDebitIsBacked(ws)
	if receives := w.ledger(ws, "debit", "email_receive:%"); len(receives) != replies || sumOf(receives) != replies*prices.perMessage {
		t.Fatalf("receive debits = %d, want %d", len(receives), replies)
	}
	if rows := inbound(w.transcript(primer.ConversationID)); len(rows) != replies {
		t.Fatalf("recorded replies = %d, want %d", len(rows), replies)
	}
	w.assertLedgerOwingOnlyForReplies(ws)
	if total := w.wallet(ws); total > ws.Start || total < -replies*prices.perMessage {
		t.Fatalf("wallet = %d outside [-%d, %d]", total, replies*prices.perMessage, ws.Start)
	}

	drained := w.wallet(ws)
	_, err = w.sendFromWorkflow(ws, direct[0], "direto:sem-saldo")
	switch {
	case drained < unit && transactionalemail.CodeOf(err) != transactionalemail.CodeBalanceInsufficient:
		t.Fatalf("a send on a wallet of %d = %v, want a balance refusal", drained, err)
	case drained >= unit:
		w.must(err)
	}
	before := w.wallet(ws)
	last := w.receive(w.replyFrom(direct[0], replyTo, "Última resposta", 0))
	if charge := w.receiveCharge(ws, last.MessageID); len(charge) != 1 || w.wallet(ws) != before-prices.perMessage {
		t.Fatalf("the last reply charged %v, wallet %d (was %d)", amounts(charge), w.wallet(ws), before)
	}
	if w.wallet(ws) < 0 && slices.Contains(w.ses.Receiving(ws.Domain.ReplyDomain), es.ReplyRecipient(ws.Domain.ReplyDomain)) {
		t.Fatal("the reply that took the wallet negative did not pause reply@")
	}
	w.assertEveryDebitIsBacked(ws)
	w.assertLedgerOwingOnlyForReplies(ws)
	t.Logf("campaign sent %d (status %s), one-to-one sent %d and refused %d for balance, wallet %d of %d", campaignSent, w.campaign(c).Status, tally.sent, tally.short, w.wallet(ws), ws.Start)
}
