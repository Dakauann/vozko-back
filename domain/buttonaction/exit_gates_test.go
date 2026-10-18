package buttonaction

import (
	"errors"
	"testing"

	"vozko/domain/whatsapp/template"
)

func TestWhichExitGatesApply(t *testing.T) {
	none, ok := Verdict{State: VerdictNone}, Verdict{State: VerdictOK}
	cases := []struct {
		category template.TemplateCategory
		verdict  Verdict
		want     ExitGates
	}{
		{template.TemplateCategoryMarketing, none, ExitGates{TemplateExitRequired: true, CampaignAcknowledgementRequired: true}},
		{template.TemplateCategoryUtility, none, ExitGates{CampaignAcknowledgementRequired: true}},
		{template.TemplateCategoryAuthentication, none, ExitGates{CampaignAcknowledgementRequired: true}},
		{template.TemplateCategoryMarketing, ok, ExitGates{}},
		{template.TemplateCategoryUtility, ok, ExitGates{}},
		{"marketing", none, ExitGates{TemplateExitRequired: true, CampaignAcknowledgementRequired: true}},
	}
	for _, tc := range cases {
		if got := ExitGatesOf(tc.category, tc.verdict); got != tc.want {
			t.Fatalf("%s %s: got %+v, want %+v", tc.category, tc.verdict.State, got, tc.want)
		}
	}
}

func TestAMarketingTemplateIsCreatedOnlyWithAShownExit(t *testing.T) {
	none, ok := Verdict{State: VerdictNone}, Verdict{State: VerdictOK}
	cases := []struct {
		category template.TemplateCategory
		verdict  Verdict
		want     error
	}{
		{template.TemplateCategoryMarketing, none, ErrMarketingNeedsExit},
		{" marketing ", none, ErrMarketingNeedsExit},
		{template.TemplateCategoryMarketing, ok, nil},
		{template.TemplateCategoryUtility, none, nil},
		{template.TemplateCategoryAuthentication, none, nil},
	}
	for _, tc := range cases {
		if err := RequireTemplateExit(tc.category, tc.verdict); !errors.Is(err, tc.want) && !(tc.want == nil && err == nil) {
			t.Errorf("%q %s: got %v, want %v", tc.category, tc.verdict.State, err, tc.want)
		}
	}
	if ErrorCode(ErrMarketingNeedsExit) != "marketing_template_needs_exit" {
		t.Fatalf("code = %q", ErrorCode(ErrMarketingNeedsExit))
	}
}
