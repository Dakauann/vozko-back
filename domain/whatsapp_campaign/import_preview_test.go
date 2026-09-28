package whatsapp_campaign_test

import (
	"testing"

	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
)

func TestImportPreviewBecomesCampaignPhoneInputs(t *testing.T) {
	p := wc.ImportPreview{ImportResult: campaign.ImportResult{Rows: []campaign.ImportRow{{Number: "5584994409624", Name: "Maria", Variables: []string{"BF10"}}}}}
	got := p.PhoneInputs()
	if len(got) != 1 || got[0].Number != "5584994409624" || got[0].Name != "Maria" || got[0].Variables[0] != "BF10" {
		t.Fatalf("inputs = %+v", got)
	}
}
