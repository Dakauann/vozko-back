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

func TestMetaMustConfirmEachStepOfTheLink(t *testing.T) {
	if err := CodeSent("VERIFICATION_CODE_SEND_SUCCESS"); err != nil {
		t.Fatal(err)
	}
	if err := LinkVerified("VERIFIED"); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"", "VERIFIED", "UNKNOWN"} {
		if err := CodeSent(status); !errors.Is(err, ErrPageLinkRefused) {
			t.Fatalf("code sent with %q: %v", status, err)
		}
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
