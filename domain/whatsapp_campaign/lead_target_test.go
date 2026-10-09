package whatsapp_campaign

import (
	"errors"
	"testing"

	"vozko/domain/campaign"
)

func leadTargeted(inputs ...PhoneInput) *Campaign {
	return &Campaign{Name: "Matrículas", TemplateID: "tpl", BusinessPhoneID: "phone", PhoneInputs: inputs}
}

func TestALeadTargetNeedsNoNumberOfItsOwn(t *testing.T) {
	c := leadTargeted(PhoneInput{LeadID: " lead-1 "}, PhoneInput{LeadID: "lead-2", Number: "not a number"})
	c.Normalize()
	if err := c.Validate(); err != nil {
		t.Fatalf("lead targets were refused: %v", err)
	}
	if c.PhoneInputs[0].LeadID != "lead-1" {
		t.Fatalf("lead id not trimmed: %q", c.PhoneInputs[0].LeadID)
	}
}

func TestLeadTargetsAreDedupedByLead(t *testing.T) {
	c := leadTargeted(PhoneInput{LeadID: "lead-1"}, PhoneInput{LeadID: "lead-1"}, PhoneInput{Number: "5511999990001"}, PhoneInput{Number: "5511999990001"})
	c.Normalize()
	if len(c.PhoneInputs) != 2 {
		t.Fatalf("inputs after normalize = %d, want 2", len(c.PhoneInputs))
	}
}

func TestANumberTargetStillNeedsAValidNumber(t *testing.T) {
	c := leadTargeted(PhoneInput{LeadID: "lead-1"}, PhoneInput{Number: "123"})
	c.Normalize()
	if err := c.Validate(); !errors.Is(err, ErrCampaignPhoneNumberInvalid) {
		t.Fatalf("Validate = %v", err)
	}
}

func TestASkippedTargetIsExemptFromTheTemplateVariables(t *testing.T) {
	c := leadTargeted(PhoneInput{LeadID: "lead-1", Variables: []string{"Maria"}}, PhoneInput{LeadID: "lead-2", Skip: campaign.SkipMissingVariable})
	if err := c.ValidateTemplateVariables(1); err != nil {
		t.Fatalf("a skipped target was checked: %v", err)
	}
	c.PhoneInputs[1].Skip = ""
	if err := c.ValidateTemplateVariables(1); !errors.Is(err, ErrCampaignTemplateVariablesMismatch) {
		t.Fatalf("an unskipped target without variables = %v", err)
	}
}

func TestTheCampaignCapIsTheSharedOne(t *testing.T) {
	if MaxCampaignPhoneNumbers != campaign.MaxEntries {
		t.Fatalf("cap = %d, want %d", MaxCampaignPhoneNumbers, campaign.MaxEntries)
	}
	if !errors.Is(ErrCampaignWorkflowVarsMissing, campaign.ErrWorkflowVarsMissing) {
		t.Fatal("the workflow vars error is not the shared one")
	}
}
