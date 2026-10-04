package advertising

import (
	"errors"
	"testing"
)

func linkablePage() RemotePage {
	return RemotePage{PageID: "p-1", BusinessID: "biz-1", WhatsAppNumber: "+55 11 90000-0000"}
}

func TestOnlyNumbersThePageCanTakeAreOffered(t *testing.T) {
	numbers := []WorkspaceNumber{
		{Kind: NumberOfficial, Number: "5511965467700", PortfolioID: "biz-1"},
		{Kind: NumberOfficial, Number: "5511911112222", PortfolioID: "biz-other"},
		{Kind: NumberOfficial, Number: "5511933334444"},
		{Kind: NumberUnofficial, Number: "5511955556666"},
		{Kind: NumberOfficial, Number: "5511900000000", PortfolioID: "biz-1"},
	}
	got := NumbersLinkableTo(linkablePage(), numbers)
	if len(got) != 2 || got[0].Number != "5511965467700" || got[1].Number != "5511955556666" {
		t.Fatalf("linkable %+v", got)
	}
}

func TestAPageOutsideAPortfolioTakesNoOfficialNumber(t *testing.T) {
	page := linkablePage()
	page.BusinessID = ""
	got := NumbersLinkableTo(page, []WorkspaceNumber{{Kind: NumberOfficial, Number: "5511965467700", PortfolioID: "biz-1"}, {Kind: NumberUnofficial, Number: "5511955556666"}})
	if len(got) != 1 || got[0].Kind != NumberUnofficial {
		t.Fatalf("linkable %+v", got)
	}
}

func TestALinkRequestNamesAPageAndANumberThePageCanTake(t *testing.T) {
	numbers := []WorkspaceNumber{{Kind: NumberOfficial, Number: "5511965467700", PortfolioID: "biz-1"}}
	number, err := LinkableNumber(linkablePage(), numbers, "+55 (11) 96546-7700")
	if err != nil || number != "5511965467700" {
		t.Fatalf("number %q err %v", number, err)
	}
	for _, raw := range []string{"", "5511911112222", "+55 11 90000-0000"} {
		if _, err := LinkableNumber(linkablePage(), numbers, raw); fieldCode(err, "number") != "not_linkable" {
			t.Fatalf("%q: %v", raw, err)
		}
	}
}

func TestTheLinkCodeIsTheDigitsMetaSent(t *testing.T) {
	for raw, want := range map[string]string{"83569": "83569", " 835 690 ": "835690", "1234": "1234", "12345678": "12345678"} {
		got, err := LinkCode(raw)
		if err != nil || got != want {
			t.Fatalf("%q: %q %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", "123", "123456789", "83a69"} {
		if _, err := LinkCode(raw); fieldCode(err, "code") != "invalid" {
			t.Fatalf("%q accepted: %v", raw, err)
		}
	}
}

func TestAskingForACodeEitherSendsItOrFindsTheNumberAlreadyLinked(t *testing.T) {
	for status, want := range map[string]LinkRequestOutcome{"VERIFICATION_CODE_SEND_SUCCESS": LinkCodeSent, "VERIFIED": LinkAlreadyLinked} {
		got, err := LinkRequested(status)
		if err != nil || got != want {
			t.Fatalf("%s: %s %v", status, got, err)
		}
	}
	for _, status := range []string{"", "UNKNOWN"} {
		if _, err := LinkRequested(status); !errors.Is(err, ErrPageLinkRefused) {
			t.Fatalf("%q: %v", status, err)
		}
	}
}

func TestARefusedLinkKeepsMetasAnswer(t *testing.T) {
	_, err := LinkRequested("NUMBER_ALREADY_IN_USE")
	var refusal *PageLinkRefusal
	if !errors.As(err, &refusal) || refusal.Answer != "NUMBER_ALREADY_IN_USE" || !errors.Is(err, ErrPageLinkRefused) {
		t.Fatalf("got %v", err)
	}
	if err := LinkVerified(""); !errors.As(err, &refusal) || refusal.Answer != "" {
		t.Fatalf("got %v", err)
	}
}

func TestMetaMustConfirmEachStepOfTheLink(t *testing.T) {
	if err := LinkVerified("VERIFIED"); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"", "VERIFICATION_CODE_SEND_SUCCESS", "INVALID_CODE"} {
		if err := LinkVerified(status); !errors.Is(err, ErrPageLinkRefused) {
			t.Fatalf("verified with %q: %v", status, err)
		}
	}
}

func fieldCode(err error, field string) string {
	var invalid *ValidationError
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

func TestALinkVozkoRecordedCountsEvenWhenMetaHidesTheNumber(t *testing.T) {
	page := RemotePage{PageID: "p-1", BusinessID: "biz-1"}
	numbers := []WorkspaceNumber{
		{Kind: NumberOfficial, Number: "+55 11 96546-7700", PortfolioID: "biz-1", LinkedPageIDs: []string{"p-1"}},
		{Kind: NumberOfficial, Number: "5511911112222", PortfolioID: "biz-1", LinkedPageIDs: []string{"p-other"}},
	}
	linked := NumbersLinkedTo(page, numbers)
	if len(linked) != 1 || linked[0].Number != "+55 11 96546-7700" {
		t.Fatalf("linked %+v", linked)
	}
	linkable := NumbersLinkableTo(page, numbers)
	if len(linkable) != 1 || linkable[0].Number != "5511911112222" {
		t.Fatalf("linkable %+v", linkable)
	}
}

func TestAWhatsAppAdNeedsANumberOfTheWorkspaceLinkedToItsPage(t *testing.T) {
	page := RemotePage{PageID: "p-1", WhatsAppNumber: "+55 11 90000-0000"}
	numbers := []WorkspaceNumber{
		{Kind: NumberOfficial, Number: "5511965467700", LinkedPageIDs: []string{"p-1"}},
		{Kind: NumberOfficial, Number: "5511900000000"},
		{Kind: NumberOfficial, Number: "5511911112222"},
	}
	for _, ok := range []string{"5511965467700", "+55 (11) 90000-0000"} {
		if err := WhatsAppDestination(page, numbers, ok); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
	if fieldCode(WhatsAppDestination(page, numbers, "5511911112222"), "adSet.whatsAppNumber") != "not_linked_to_page" {
		t.Fatal("an unlinked number was accepted")
	}
	if fieldCode(WhatsAppDestination(page, numbers, "5511977778888"), "adSet.whatsAppNumber") != "not_in_workspace" {
		t.Fatal("a number outside the workspace was accepted")
	}
	outside := RemotePage{PageID: "p-1", WhatsAppNumber: "5511977778888"}
	if fieldCode(WhatsAppDestination(outside, numbers, "5511977778888"), "adSet.whatsAppNumber") != "not_in_workspace" {
		t.Fatal("a number linked on Meta but not connected to Vozko was accepted")
	}
}
