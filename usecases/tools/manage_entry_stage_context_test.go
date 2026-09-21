package tools_usecase

import (
	"testing"

	"vozko/domain/stage"
	"vozko/domain/tools"
)

type stubStageRepo struct {
	stage.Repository
	gotWorkspace, gotCampaign, gotCampaignType string
	stages                                     []*stage.Stage
}

func (r *stubStageRepo) ListByCampaign(workspaceID, campaignID, campaignType string) ([]*stage.Stage, error) {
	r.gotWorkspace, r.gotCampaign, r.gotCampaignType = workspaceID, campaignID, campaignType
	return r.stages, nil
}

func stagesFixture() []*stage.Stage {
	return []*stage.Stage{
		{Name: "Novo", Description: "ainda não respondeu"},
		{Name: "Qualificado", Description: "demonstrou interesse"},
	}
}

func TestStageEnumIsPopulatedWithoutACampaign(t *testing.T) {
	repo := &stubStageRepo{stages: stagesFixture()}
	tool := &manageEntryStageTool{stageRepo: repo}

	def := tool.DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws-1"})

	enum := def.Parameters["target_tag_name"].Enum
	if len(enum) != 2 || enum[0] != "Novo" {
		t.Fatalf("enum = %v, want the workspace's default pipeline stages", enum)
	}
	if repo.gotWorkspace != "ws-1" || repo.gotCampaign != "" {
		t.Errorf("lookup = ws=%q campaign=%q, want an empty campaign passed through",
			repo.gotWorkspace, repo.gotCampaign)
	}
}

func TestStageEnumStillHonoursACampaignsOwnPipeline(t *testing.T) {
	repo := &stubStageRepo{stages: stagesFixture()}
	tool := &manageEntryStageTool{stageRepo: repo}

	tool.DefinitionWithContext(tools.ToolContext{
		WorkspaceID:  "ws-1",
		CampaignID:   "camp-1",
		CampaignType: "whatsapp",
	})

	if repo.gotCampaign != "camp-1" || repo.gotCampaignType != "whatsapp" {
		t.Errorf("campaign = %q/%q, want it forwarded", repo.gotCampaign, repo.gotCampaignType)
	}
}

func TestStageEnumIsOmittedWithoutAWorkspace(t *testing.T) {
	repo := &stubStageRepo{stages: stagesFixture()}
	tool := &manageEntryStageTool{stageRepo: repo}

	def := tool.DefinitionWithContext(tools.ToolContext{})

	if len(def.Parameters["target_tag_name"].Enum) != 0 {
		t.Error("an enum was built without a workspace to read stages from")
	}
}

func TestStageEnumIsOmittedWhenNoStagesExist(t *testing.T) {
	repo := &stubStageRepo{stages: nil}
	tool := &manageEntryStageTool{stageRepo: repo}

	def := tool.DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws-1"})

	if len(def.Parameters["target_tag_name"].Enum) != 0 {
		t.Error("expected the bare definition when the pipeline has no stages")
	}
}
