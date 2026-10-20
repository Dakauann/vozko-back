package facebook

import (
	"context"
	"testing"
	"time"

	"vozko/domain/conversation"
)

func TestTheAdapterGivesAContinuationTheDisclosureTheInboundReplyUses(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	var disclosing conversation.DisclosingAdapter = f.adapter

	got, err := disclosing.AutomationDisclosure(context.Background(), f.ec)
	page, _ := f.pages.FindByID(context.Background(), "page-1")
	if err != nil || got == "" || got != page.Automation().Disclosure {
		t.Fatalf("disclosure = %q, %v; want the page default %q", got, err, page.Automation().Disclosure)
	}

	f.pages.byID["page-1"].AutomationDisclosure = "Atendimento automático da Loja."
	if got, err := disclosing.AutomationDisclosure(context.Background(), f.ec); err != nil || got != "Atendimento automático da Loja." {
		t.Fatalf("disclosure = %q, %v; want the page's own text", got, err)
	}

	gone := *f.ec
	gone.AccountID = "page-gone"
	if _, err := disclosing.AutomationDisclosure(context.Background(), &gone); err == nil {
		t.Fatal("an unreadable page must not yield an empty disclosure")
	}
}
