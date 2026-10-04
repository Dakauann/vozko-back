package advertising

import (
	"context"
	"errors"
	"testing"

	ads "vozko/domain/advertising"
)

func linkWorld() *world {
	w := newWorld()
	w.gateway.pages = []ads.RemotePage{{PageID: "page-1", BusinessID: "biz-1", Name: "Loja", CanAdvertise: true}}
	w.numbers.numbers = []ads.WorkspaceNumber{
		{Kind: ads.NumberOfficial, Label: "Loja", Number: "5511965467700", PortfolioID: "biz-1"},
		{Kind: ads.NumberOfficial, Label: "Outra empresa", Number: "5511911112222", PortfolioID: "biz-9"},
	}
	w.gateway.linkStatus = "VERIFICATION_CODE_SEND_SUCCESS"
	return w
}

func (w *world) assets() *AssetsUseCase { return NewAssetsUseCase(w.sync, w.gateway, w.numbers) }

func TestPagesOfferTheNumbersEachPageCanTake(t *testing.T) {
	w := linkWorld()
	pages, err := w.assets().Pages(context.Background(), "ws-1", "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || len(pages[0].Numbers) != 0 || len(pages[0].Linkable) != 1 || pages[0].Linkable[0].Number != "5511965467700" {
		t.Fatalf("pages %+v", pages)
	}
	if len(pages[0].Capabilities) == 0 || pages[0].Capabilities[1].Channel != ads.PageWhatsApp || pages[0].Capabilities[1].Action.InApp != ads.ActionLinkWhatsApp {
		t.Fatalf("capabilities %+v", pages[0].Capabilities)
	}
}

func TestAskingForTheLinkCodeGoesToMetaOnlyForANumberThePageCanTake(t *testing.T) {
	w := linkWorld()
	outcome, err := w.assets().RequestNumberLink(context.Background(), "ws-1", "acc-1", "page-1", "+55 11 96546-7700")
	if err != nil || outcome.Outcome != ads.LinkCodeSent || outcome.Page != nil {
		t.Fatalf("outcome %+v err %v", outcome, err)
	}
	if w.gateway.linked.number != "5511965467700" || w.gateway.linked.code != "" || w.gateway.linked.page != "page-1" {
		t.Fatalf("linked %+v", w.gateway.linked)
	}
	w2 := linkWorld()
	_, err = w2.assets().RequestNumberLink(context.Background(), "ws-1", "acc-1", "page-1", "5511911112222")
	var invalid *ads.ValidationError
	if !errors.As(err, &invalid) || w2.gateway.linked.number != "" {
		t.Fatalf("a number from another portfolio reached Meta: %v", err)
	}
}

func TestAPageOutsideTheConnectionCannotBeLinked(t *testing.T) {
	w := linkWorld()
	_, err := w.assets().RequestNumberLink(context.Background(), "ws-1", "acc-1", "page-9", "5511965467700")
	if !errors.Is(err, ads.ErrPageNotGranted) || w.gateway.linked.number != "" {
		t.Fatalf("got %v", err)
	}
}

func TestACodeMetaDidNotSendIsAnError(t *testing.T) {
	w := linkWorld()
	w.gateway.linkStatus = "UNKNOWN"
	if _, err := w.assets().RequestNumberLink(context.Background(), "ws-1", "acc-1", "page-1", "5511965467700"); !errors.Is(err, ads.ErrPageLinkRefused) {
		t.Fatalf("got %v", err)
	}
}

func TestConfirmingTheLinkSendsTheCodeAndReturnsThePageAgain(t *testing.T) {
	w := linkWorld()
	w.gateway.linkStatus = "VERIFIED"
	page, err := w.assets().ConfirmNumberLink(context.Background(), "ws-1", "acc-1", "page-1", "5511965467700", " 83569 ")
	if err != nil {
		t.Fatal(err)
	}
	if w.gateway.linked.code != "83569" || page.Page.PageID != "page-1" {
		t.Fatalf("linked %+v page %+v", w.gateway.linked, page)
	}
	if len(page.Numbers) != 1 || page.Numbers[0].Number != "5511965467700" || len(page.Linkable) != 0 {
		t.Fatalf("Meta hides the linked number, but Vozko should count the link it made: %+v", page)
	}
}

func TestALinkThatCannotBeRecordedIsReported(t *testing.T) {
	w := linkWorld()
	w.gateway.linkStatus = "VERIFIED"
	w.numbers.recordErr = errors.New("db down")
	if _, err := w.assets().ConfirmNumberLink(context.Background(), "ws-1", "acc-1", "page-1", "5511965467700", "83569"); err == nil {
		t.Fatal("an unrecorded link was reported as done")
	}
}

func TestAWhatsAppAdPublishesWithTheLinkVozkoRecorded(t *testing.T) {
	w := newWorld()
	w.gateway.pages[0].WhatsAppNumber = ""
	w.numbers.numbers[0].LinkedPageIDs = []string{"page-1"}
	if _, err := w.publisher().Check(context.Background(), "ws-1", publishableDraft()); err != nil {
		t.Fatalf("a recorded link was refused: %v", err)
	}
	w.numbers.numbers[0].LinkedPageIDs = nil
	if _, err := w.publisher().Check(context.Background(), "ws-1", publishableDraft()); fieldIssue(err, "adSet.whatsAppNumber") != "not_linked_to_page" {
		t.Fatalf("an unlinked number passed: %v", err)
	}
}

func TestAWrongLinkCodeNeverReachesMeta(t *testing.T) {
	w := linkWorld()
	_, err := w.assets().ConfirmNumberLink(context.Background(), "ws-1", "acc-1", "page-1", "5511965467700", "12")
	var invalid *ads.ValidationError
	if !errors.As(err, &invalid) || w.gateway.linked.number != "" {
		t.Fatalf("got %v", err)
	}
}

func TestACodeMetaRejectsIsNotALink(t *testing.T) {
	w := linkWorld()
	w.gateway.linkStatus = "INVALID"
	if _, err := w.assets().ConfirmNumberLink(context.Background(), "ws-1", "acc-1", "page-1", "5511965467700", "83569"); !errors.Is(err, ads.ErrPageLinkRefused) {
		t.Fatalf("got %v", err)
	}
}

func TestLinkingNeedsAnAccountThatCanAdvertise(t *testing.T) {
	w := linkWorld()
	w.accounts.byID["acc-1"].Tasks = []string{"ANALYZE"}
	if _, err := w.assets().RequestNumberLink(context.Background(), "ws-1", "acc-1", "page-1", "5511965467700"); err == nil || w.gateway.linked.number != "" {
		t.Fatalf("a read-only account linked a number: %v", err)
	}
}

func fieldIssue(err error, field string) string {
	var invalid *ads.ValidationError
	if !errors.As(err, &invalid) {
		return ""
	}
	for _, issue := range invalid.Issues {
		if issue.Field == field {
			return issue.Code
		}
	}
	return ""
}

func TestANumberMetaAlreadyLinkedIsRecordedWithoutACode(t *testing.T) {
	w := linkWorld()
	w.gateway.linkStatus = "VERIFIED"
	outcome, err := w.assets().RequestNumberLink(context.Background(), "ws-1", "acc-1", "page-1", "5511965467700")
	if err != nil || outcome.Outcome != ads.LinkAlreadyLinked || outcome.Page == nil {
		t.Fatalf("outcome %+v err %v", outcome, err)
	}
	if len(outcome.Page.Numbers) != 1 || outcome.Page.Numbers[0].Number != "5511965467700" {
		t.Fatalf("page %+v", outcome.Page)
	}
}
