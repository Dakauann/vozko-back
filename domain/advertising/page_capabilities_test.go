package advertising

import (
	"net/url"
	"strings"
	"testing"
)

func capabilityOf(caps []PageCapability, channel PageChannel) PageCapability {
	for _, c := range caps {
		if c.Channel == channel {
			return c
		}
	}
	return PageCapability{}
}

func readyPage() RemotePage {
	return RemotePage{
		PageID: "p-1", BusinessID: "biz-1", Name: "Loja", CanAdvertise: true, LeadTermsAccepted: true,
		InstagramUserID: "ig-1", InstagramUsername: "loja",
	}
}

func linkedNumbers() []WorkspaceNumber {
	return []WorkspaceNumber{{Kind: NumberOfficial, Number: "5511965467700", PortfolioID: "biz-1", LinkedPageIDs: []string{"p-1"}}}
}

func TestAReadyPageListsEveryMetaChannelInOrder(t *testing.T) {
	caps := PageCapabilities(readyPage(), linkedNumbers())
	want := []PageChannel{PageAdvertise, PageWhatsApp, PageInstagram, PageMessenger, PageLeadForms}
	if len(caps) != len(want) {
		t.Fatalf("caps %+v", caps)
	}
	for i, c := range caps {
		if c.Channel != want[i] || c.State != StateReady || c.Action != (ReadinessAction{}) {
			t.Fatalf("%d: %+v", i, c)
		}
	}
}

func TestAPageWithoutAdvertisingRightsPointsToItsPortfolioRoles(t *testing.T) {
	page := readyPage()
	page.CanAdvertise = false
	c := capabilityOf(PageCapabilities(page, nil), PageAdvertise)
	if c.State != StateMissing || !hasQuery(c.Action.Portal, "selected_asset_id", "p-1") || !hasQuery(c.Action.Portal, "business_id", "biz-1") {
		t.Fatalf("advertise %+v", c)
	}
	page.BusinessID = ""
	if c := capabilityOf(PageCapabilities(page, nil), PageAdvertise); c.Action.Portal != PageAccessPortalURL {
		t.Fatalf("personal page %+v", c)
	}
}

func TestWhatsAppIsLinkedInVozkoWhenANumberCanTakeThePage(t *testing.T) {
	page := readyPage()
	numbers := []WorkspaceNumber{{Kind: NumberOfficial, Number: "5511965467700", PortfolioID: "biz-1"}}
	c := capabilityOf(PageCapabilities(page, numbers), PageWhatsApp)
	if c.State != StateMissing || c.Action.InApp != ActionLinkWhatsApp || c.Action.Portal != "" {
		t.Fatalf("whatsapp %+v", c)
	}
}

func TestWhatsAppNeedsANumberConnectedToVozkoFirst(t *testing.T) {
	page := readyPage()
	page.WhatsAppNumber = "+55 11 90000-0000"
	for _, numbers := range [][]WorkspaceNumber{nil, {{Kind: NumberOfficial, Number: "5511911112222", PortfolioID: "biz-9"}}} {
		c := capabilityOf(PageCapabilities(page, numbers), PageWhatsApp)
		if c.State != StateMissing || c.Action.InApp != ActionConnectWhatsApp {
			t.Fatalf("%v: %+v", numbers, c)
		}
	}
}

func TestInstagramAndLeadTermsAreFixedAtMeta(t *testing.T) {
	page := readyPage()
	page.InstagramUserID, page.InstagramUsername, page.LeadTermsAccepted = "", "", false
	caps := PageCapabilities(page, linkedNumbers())
	c := capabilityOf(caps, PageInstagram)
	if c.State != StateMissing || !hasQuery(c.Action.Portal, "asset_id", page.PageID) || !hasQuery(c.Action.Portal, "business_id", page.BusinessID) {
		t.Fatalf("instagram %+v", c)
	}
	page.BusinessID = ""
	if portal := capabilityOf(PageCapabilities(page, linkedNumbers()), PageInstagram).Action.Portal; !hasQuery(portal, "asset_id", page.PageID) || strings.Contains(portal, "business_id") {
		t.Fatalf("instagram without business %q", portal)
	}
	if c := capabilityOf(caps, PageLeadForms); c.State != StateMissing || c.Action.Portal != LeadTermsPortalURL {
		t.Fatalf("lead forms %+v", c)
	}
	if c := capabilityOf(caps, PageMessenger); c.State != StateReady {
		t.Fatalf("messenger %+v", c)
	}
}

func hasQuery(raw, key, value string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Query().Get(key) == value
}
