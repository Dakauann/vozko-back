package tiktok

import (
	"context"
	"testing"

	"vozko/domain/conversation"
)

func TestTheAdapterGivesAContinuationTheDisclosureTheInboundReplyUses(t *testing.T) {
	h := newAdapterHarness(t)
	h.account.AutomationDisclosure = "  Sou a assistente virtual da loja.  "
	h.accounts.put(h.account)
	var disclosing conversation.DisclosingAdapter = h.a

	got, err := disclosing.AutomationDisclosure(context.Background(), h.ec)
	if err != nil || got != h.account.Automation().Disclosure || got != "Sou a assistente virtual da loja." {
		t.Fatalf("disclosure = %q, %v", got, err)
	}

	gone := *h.ec
	gone.AccountID = "account-gone"
	if _, err := disclosing.AutomationDisclosure(context.Background(), &gone); err == nil {
		t.Fatal("an unreadable account must not yield an empty disclosure")
	}
}
