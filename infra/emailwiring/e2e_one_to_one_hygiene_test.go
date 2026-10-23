package emailwiring_test

import (
	"testing"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	h "vozko/domain/emailhygiene"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	conversation_repository "vozko/infra/repositories/conversation"
	conversation_usecase "vozko/usecases/conversation"
	"vozko/usecases/transactionalemail"
)

func TestEmailE2E_AReplyToADomainThatStoppedAcceptingMailIsRefusedAndRefunded(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Higiene", roomyWallet)
	ana := schema.Lead{ID: uuid.NewString(), WorkspaceID: ws.ID, Name: "Ana", Email: schema.OptionalText("ana@lento-" + ws.ID[:8] + ".example")}
	w.must(w.db.Create(&ana).Error)
	first, err := w.sendFromWorkflow(ws, ana, "pedido:"+ana.ID)
	w.must(err)
	walletBefore := w.wallet(ws)

	domain := emailaddress.Address(ana.Email).Domain()
	w.resolver.set(domain, h.DNSAnswer{MX: h.LookupTimedOut})
	w.must(w.db.Exec(`UPDATE email_domain_checks SET expires_at = now() - interval '1 hour' WHERE domain = ?`, domain).Error)

	sender := conversation_usecase.NewMessageSenderService(conversation_repository.NewRepository(w.db), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	sender.SetChannelAdapters(conversation.NewAdapterRegistry(w.inbox.Adapter))
	_, err = sender.SendTextMessage(first.ConversationID, string(shared.EntryTypeEmail), "Seguem as fotos.", conversation.SendOrigin{SenderUserID: ws.Actor.UserID})

	if code := conversation.SendFailureCode(err, "send_failed"); code != h.CodeDomainUnknown {
		t.Fatalf("reply = %v (%s), want %s", err, code, h.CodeDomainUnknown)
	}
	if sends := w.ses.SentTo(emailaddress.Address(ana.Email)); len(sends) != 1 {
		t.Fatalf("provider sends to ana = %d, the refused reply reached the provider", len(sends))
	}
	debits := w.ledger(ws, "debit", "email_message:email_reply:"+first.ConversationID+":%")
	refunds := w.ledger(ws, "credit", "refund:email_message:email_reply:"+first.ConversationID+":%")
	if sumOf(debits) != sumOf(refunds) || w.wallet(ws) != walletBefore {
		t.Fatalf("debits %v refunds %v wallet %d (was %d), a refused reply costs nothing", amounts(debits), amounts(refunds), w.wallet(ws), walletBefore)
	}
	w.assertLedger(ws)
}

func TestEmailE2E_AOneToOneSendToADisposableAddressIsRefusedForFree(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Descartavel", roomyWallet)
	bia := schema.Lead{ID: uuid.NewString(), WorkspaceID: ws.ID, Name: "Bia", Email: schema.OptionalText("bia" + ws.ID[:8] + "@mailinator.com")}
	w.must(w.db.Create(&bia).Error)
	walletBefore := w.wallet(ws)

	_, err := w.sendFromWorkflow(ws, bia, "pedido:"+bia.ID)
	if code := transactionalemail.CodeOf(err); code != h.CodeDisposableAddress {
		t.Fatalf("send = %v (%s), want %s", err, code, h.CodeDisposableAddress)
	}
	_, err = w.startOutreach(ws, bia, "compose-descartavel")
	if code := transactionalemail.CodeOf(err); code != h.CodeDisposableAddress {
		t.Fatalf("outreach = %v (%s), want %s", err, code, h.CodeDisposableAddress)
	}
	if sends := w.ses.SentTo(emailaddress.Address(bia.Email)); len(sends) != 0 {
		t.Fatalf("provider sends = %d, a disposable address is never mailed", len(sends))
	}
	debits, refunds := w.oneToOneCharges(ws)
	if sumOf(debits) != sumOf(refunds) || w.wallet(ws) != walletBefore {
		t.Fatalf("debits %v refunds %v wallet %d (was %d)", amounts(debits), amounts(refunds), w.wallet(ws), walletBefore)
	}
	w.assertLedger(ws)
}
