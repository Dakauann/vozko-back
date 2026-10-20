package mercadolivre

import (
	"context"
	"testing"

	"vozko/domain/conversation"
)

func TestTheAdapterGivesAContinuationTheDisclosureTheInboundReplyUses(t *testing.T) {
	f := newAdapterFixture(t, false, true)
	account := f.accounts.get("acct-1")
	account.AutomationDisclosure = "Mensagem automática da loja."
	f.accounts.put(account)
	var disclosing conversation.DisclosingAdapter = f.adapter

	got, err := disclosing.AutomationDisclosure(context.Background(), f.entry(t))
	if err != nil || got != account.Automation().Disclosure || got != "Mensagem automática da loja." {
		t.Fatalf("disclosure = %q, %v", got, err)
	}

	gone := *f.entry(t)
	gone.AccountID = "acct-gone"
	if _, err := disclosing.AutomationDisclosure(context.Background(), &gone); err == nil {
		t.Fatal("an unreadable account must not yield an empty disclosure")
	}
}
