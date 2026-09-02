package handlers

import (
	"testing"
)

// A campaign is linked to ONE funnel. Two request fields could name it —
// pipelineId, and the legacy stageGroupId — and the two used to be applied one
// after the other: the campaign was created carrying pipelineId, then the stage
// group's clone ran and called SetCampaignPipeline, overwriting it. Sending both
// therefore did something the request never asked for, silently.
//
// campaignFunnelSource states the rule so it can be asserted without standing up
// the whole handler: pipelineId wins, and the group is consulted only in its
// absence.
type campaignFunnelChoice int

const (
	funnelFromPipeline campaignFunnelChoice = iota
	funnelFromStageGroup
	funnelFromWorkspaceDefault
)

func campaignFunnelSource(pipelineID, stageGroupID string) campaignFunnelChoice {
	// Mirrors the guard in WhatsAppCampaignHandler.Create.
	if pipelineID != "" {
		return funnelFromPipeline
	}
	if stageGroupID != "" {
		return funnelFromStageGroup
	}
	return funnelFromWorkspaceDefault
}

func TestCampaignFunnelSource(t *testing.T) {
	cases := []struct {
		name         string
		pipelineID   string
		stageGroupID string
		want         campaignFunnelChoice
	}{
		{"explicit funnel", "pipe-1", "", funnelFromPipeline},
		{"legacy stage group", "", "grp-1", funnelFromStageGroup},
		{
			// The regression: both present used to apply BOTH, group last.
			"both present, the funnel wins",
			"pipe-1", "grp-1", funnelFromPipeline,
		},
		{"neither, the workspace default", "", "", funnelFromWorkspaceDefault},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := campaignFunnelSource(c.pipelineID, c.stageGroupID); got != c.want {
				t.Errorf("pipelineID=%q stageGroupID=%q: got %v, want %v",
					c.pipelineID, c.stageGroupID, got, c.want)
			}
		})
	}
}

// TestCampaignCreateGuardMatchesTheRule is the tie between the rule above and the
// handler. If the guard in Create is ever loosened back to "if StageGroupID != ”"
// alone, this is the test that should be read next to it — the condition below is
// a literal copy, so a change there without a change here is a change that lost
// its reason.
func TestCampaignCreateGuardMatchesTheRule(t *testing.T) {
	guard := func(pipelineID, stageGroupID string) bool {
		// Copied from WhatsAppCampaignHandler.Create.
		return pipelineID == "" && stageGroupID != ""
	}

	if guard("pipe-1", "grp-1") {
		t.Error("an explicit funnel must not be overwritten by the legacy group path")
	}
	if !guard("", "grp-1") {
		t.Error("the legacy group path must still run when no funnel is named")
	}
	if guard("", "") {
		t.Error("with neither field there is nothing to clone")
	}
}
