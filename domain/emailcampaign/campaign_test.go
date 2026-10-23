package emailcampaign

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/emaildesign"
	"vozko/domain/selection"
)

func TestDraftValidate(t *testing.T) {
	filter := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsFalse}}}}}
	cases := []struct {
		name    string
		draft   Draft
		wantErr error
	}{
		{name: "a name is enough", draft: Draft{Name: "Black Friday"}},
		{name: "no name", draft: Draft{Name: "  "}, wantErr: ErrNameRequired},
		{name: "long name", draft: Draft{Name: strings.Repeat("a", MaxNameRunes+1)}, wantErr: ErrNameTooLong},
		{name: "valid ab", draft: Draft{Name: "x", ABTest: &ABTest{VariantBSubject: "b", TestPercent: 10, WinnerMetric: WinnerOpens, DecideAfterMinutes: 30}}},
		{name: "invalid ab", draft: Draft{Name: "x", ABTest: &ABTest{VariantBSubject: "b", TestPercent: 5, WinnerMetric: WinnerOpens, DecideAfterMinutes: 30}}, wantErr: ErrABTestPercent},
		{name: "picked leads", draft: Draft{Name: "x", Selection: &selection.Selection{Mode: selection.ModeIDs, IDs: []string{"a"}}}},
		{name: "filtered audience", draft: Draft{Name: "x", Selection: &selection.Selection{Mode: selection.ModeAllMatching, Filter: &filter}}},
		{name: "broken audience", draft: Draft{Name: "x", Selection: &selection.Selection{Mode: selection.ModeIDs}}, wantErr: ErrAudienceInvalid},
		{name: "long preheader", draft: Draft{Name: "x", Preheader: strings.Repeat("p", MaxPreheaderRunes+1)}, wantErr: ErrPreheaderTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.draft.Validate(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestApplyDraftKeepsSystemFields(t *testing.T) {
	c := Campaign{ID: "c1", WorkspaceID: "ws", Status: StatusDraft, CreatedBy: "u1", LaunchedBy: "u2"}
	ab := &ABTest{VariantBSubject: "b", TestPercent: 10, WinnerMetric: WinnerOpens, DecideAfterMinutes: 30}
	c.Apply(Draft{Name: " Nome ", Subject: "Assunto", SenderID: "s1", ABTest: ab})
	if c.Name != "Nome" || c.Subject != "Assunto" || c.SenderID != "s1" || c.ABTest == nil || c.ID != "c1" || c.CreatedBy != "u1" || c.LaunchedBy != "u2" {
		t.Fatalf("campaign = %+v", c)
	}
}

func TestCampaignUTM(t *testing.T) {
	c := Campaign{Name: "Black Friday 2026: Ofertas Únicas!"}
	got := c.TrackingUTM()
	if got.Source != "email" || got.Medium != "email" || got.Campaign != "black-friday-2026-ofertas-unic" {
		t.Fatalf("utm = %+v", got)
	}
	c.UTM = &emaildesign.UTM{Campaign: "bf26", Content: "hero"}
	got = c.TrackingUTM()
	if got.Source != "email" || got.Medium != "email" || got.Campaign != "bf26" || got.Content != "hero" {
		t.Fatalf("explicit utm = %+v", got)
	}
	if (Campaign{Name: "!!!"}).TrackingUTM().Campaign != "campanha" {
		t.Fatal("a name without letters still yields a campaign tag")
	}
}

func TestRenderDocumentAppliesThePreheader(t *testing.T) {
	c := Campaign{Preheader: "Só hoje", Document: emaildesign.Document{Settings: emaildesign.Settings{Preheader: "antigo"}}}
	if c.RenderDocument().Settings.Preheader != "Só hoje" {
		t.Fatal("the campaign preheader wins")
	}
	c.Preheader = ""
	if c.RenderDocument().Settings.Preheader != "antigo" {
		t.Fatal("without a campaign preheader the document keeps its own")
	}
}
