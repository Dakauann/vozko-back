package unofficial_whatsapp_campaign

import (
	"testing"

	"vozko/domain/campaign"
)

func TestNormalizeTargetFollowsTheLeadNumberRule(t *testing.T) {
	cases := map[string]string{
		"+55 (84) 99440-9624": "5584994409624",
		"(84) 99440-9624":     "5584994409624",
		"84 9944-0962":        "558499440962",
		"351912345678":        "",
		"123":                 "",
		"":                    "",
		"1234567890123456":    "",
	}
	for raw, want := range cases {
		if got := NormalizeTarget(raw); got != want {
			t.Errorf("%q = %q, want %q", raw, got, want)
		}
	}
}

func TestImportPreviewBecomesCampaignTargets(t *testing.T) {
	p := ImportPreview{ImportResult: campaign.ImportResult{Rows: []campaign.ImportRow{{Number: "5584994409624", Name: "Maria", Variables: []string{"BF10"}}}}}
	got := p.Targets()
	if len(got) != 1 || got[0].Number != "5584994409624" || got[0].Name != "Maria" || got[0].Variables[0] != "BF10" {
		t.Fatalf("targets = %+v", got)
	}
}
