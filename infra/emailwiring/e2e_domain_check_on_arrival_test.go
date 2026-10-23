package emailwiring_test

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	h "vozko/domain/emailhygiene"
	"vozko/domain/shared"
	conversation_repository "vozko/infra/repositories/conversation"
	"vozko/infra/ses/sesfake"
	conversation_usecase "vozko/usecases/conversation"
)

type arrival struct {
	w        *world
	ws       *tenant
	customer emailaddress.Address
	conv     string
	sender   *conversation_usecase.MessageSenderService
}

func newArrival(t *testing.T, name string) *arrival {
	t.Helper()
	w := newWorldCheckingDomainsInTheBackground(t)
	ws := w.sendingTenant(name, roomyWallet)
	customer := emailaddress.Address("carla@nunca-vista-" + ws.ID[:8] + ".example")
	w.receive(sesfake.Inbound{
		From: "Carla <" + string(customer) + ">", To: []emailaddress.Address{emailaddress.Address("reply@" + ws.Domain.ReplyDomain)},
		Subject: "Orçamento", Text: "Vocês entregam em Campinas?", SenderIP: customerIP,
	})
	sender := conversation_usecase.NewMessageSenderService(conversation_repository.NewRepository(w.db), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	sender.SetChannelAdapters(conversation.NewAdapterRegistry(w.inbox.Adapter))
	return &arrival{w: w, ws: ws, customer: customer, conv: w.conversationOf(ws, customer), sender: sender}
}

func (a *arrival) reply() error {
	_, err := a.sender.SendTextMessage(a.conv, string(shared.EntryTypeEmail), "Entregamos, sim.", conversation.SendOrigin{SenderUserID: a.ws.Actor.UserID})
	return err
}

func (a *arrival) runCheckJob(t *testing.T) {
	t.Helper()
	checks, err := a.w.hygiene.NewDomainCheckConsumer(a.w.queue)
	a.w.must(err)
	a.w.must(checks.Start())
	a.w.queue.drain(t, h.DomainCheckTopic, 1, 0)
}

func (a *arrival) assertRepliedOnce(t *testing.T) {
	t.Helper()
	if sends := a.w.ses.SentTo(a.customer); len(sends) != 1 {
		t.Fatalf("provider sends to the customer = %d, want one reply", len(sends))
	}
	a.w.assertLedgerOwingOnlyForReplies(a.ws)
}

func TestEmailE2E_ACustomerFromANeverSeenDomainGetsTheOperatorReplyOnceTheCheckJobRan(t *testing.T) {
	a := newArrival(t, "Loja Chegada")
	if queued := a.w.queue.readyCount(h.DomainCheckTopic); queued == 0 {
		t.Fatal("the arrival of the email queued no domain check job")
	}
	a.runCheckJob(t)
	if err := a.reply(); err != nil {
		t.Fatalf("the operator reply = %v (%s), want it sent with no refusal", err, conversation.SendFailureCode(err, "send_failed"))
	}
	a.assertRepliedOnce(t)
}

func TestEmailE2E_AReplyRefusedWhileTheDomainIsCheckedGoesOutOnRetryAfterTheJob(t *testing.T) {
	a := newArrival(t, "Loja Retentativa")
	err := a.reply()
	if conversation.SendFailureCode(err, "send_failed") != h.CodeDomainChecking || !h.RetryableRefusal(err) {
		t.Fatalf("reply before the check job = %v, want the retryable %s", err, h.CodeDomainChecking)
	}
	a.runCheckJob(t)
	if err := a.reply(); err != nil {
		t.Fatalf("the retry after the check job = %v (%s), want it sent", err, conversation.SendFailureCode(err, "send_failed"))
	}
	a.assertRepliedOnce(t)
}
