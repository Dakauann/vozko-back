package unofficial_whatsapp_campaign

import (
	"errors"
	"testing"

	"vozko/domain/campaign"
)

func leadTargeted(targets ...TargetInput) *Campaign {
	return &Campaign{Name: "Matrículas", InstanceID: "instance", Message: MessageSpec{Kind: KindText, Bodies: []string{"Olá {{1}}"}}, Targets: targets}
}

func TestALeadTargetNeedsNoNumberOfItsOwn(t *testing.T) {
	c := leadTargeted(TargetInput{LeadID: " lead-1 "}, TargetInput{LeadID: "lead-2"})
	c.Normalize()
	if err := c.Validate(); err != nil {
		t.Fatalf("lead targets were refused: %v", err)
	}
	if c.Targets[0].LeadID != "lead-1" {
		t.Fatalf("lead id not trimmed: %q", c.Targets[0].LeadID)
	}
}

func TestLeadTargetsAreDedupedByLead(t *testing.T) {
	c := leadTargeted(TargetInput{LeadID: "lead-1"}, TargetInput{LeadID: "lead-1"})
	c.Normalize()
	if len(c.Targets) != 1 {
		t.Fatalf("targets after normalize = %d, want 1", len(c.Targets))
	}
}

func TestASkippedTargetIsExemptFromTheMessageVariables(t *testing.T) {
	c := leadTargeted(TargetInput{LeadID: "lead-1", Variables: []string{"Maria"}}, TargetInput{LeadID: "lead-2", Skip: campaign.SkipMissingVariable})
	if err := c.ValidateTargetVariables(1); err != nil {
		t.Fatalf("a skipped target was checked: %v", err)
	}
	c.Targets[1].Skip = ""
	if err := c.ValidateTargetVariables(1); !errors.Is(err, ErrCampaignVariablesMismatch) {
		t.Fatalf("an unskipped target without variables = %v", err)
	}
}

func TestTheCampaignCapIsTheSharedOne(t *testing.T) {
	if MaxCampaignTargets != campaign.MaxEntries {
		t.Fatalf("cap = %d, want %d", MaxCampaignTargets, campaign.MaxEntries)
	}
	if !errors.Is(ErrCampaignWorkflowVarsMissing, campaign.ErrWorkflowVarsMissing) {
		t.Fatal("the workflow vars error is not the shared one")
	}
}
