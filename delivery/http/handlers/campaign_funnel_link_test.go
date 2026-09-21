package handlers

import (
	"testing"
)

type campaignFunnelChoice int

const (
	funnelFromPipeline campaignFunnelChoice = iota
	funnelFromStageGroup
	funnelFromWorkspaceDefault
)

func campaignFunnelSource(pipelineID, stageGroupID string) campaignFunnelChoice {
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

func TestCampaignCreateGuardMatchesTheRule(t *testing.T) {
	guard := func(pipelineID, stageGroupID string) bool {
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
