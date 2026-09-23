package tools_usecase

import (
	"testing"

	"vozko/domain/stage"
	"vozko/domain/tools"
)

type placementStageRepo struct {
	stage.Repository

	entryStage   *stage.EntryStage
	stageByID    map[string]*stage.Stage
	byPipeline   map[string][]*stage.Stage
	campaignList []*stage.Stage

	askedPipeline string
	askedCampaign bool
}

func (r *placementStageRepo) GetEntryStage(string, string, string) (*stage.EntryStage, error) {
	return r.entryStage, nil
}

func (r *placementStageRepo) FindByID(id string) (*stage.Stage, error) {
	return r.stageByID[id], nil
}

func (r *placementStageRepo) ListByPipeline(_ string, pipelineID string) ([]*stage.Stage, error) {
	r.askedPipeline = pipelineID
	return r.byPipeline[pipelineID], nil
}

func (r *placementStageRepo) ListByCampaign(string, string, string) ([]*stage.Stage, error) {
	r.askedCampaign = true
	return r.campaignList, nil
}

func movedEntryRepo() *placementStageRepo {
	return &placementStageRepo{
		entryStage: &stage.EntryStage{StageID: "stage-b1"},
		stageByID: map[string]*stage.Stage{
			"stage-b1": {ID: "stage-b1", Name: "Em negociacao", PipelineID: "funnel-b"},
		},
		byPipeline: map[string][]*stage.Stage{
			"funnel-b": {
				{Name: "Em negociacao", PipelineID: "funnel-b"},
				{Name: "Fechado", PipelineID: "funnel-b"},
			},
		},
		campaignList: []*stage.Stage{
			{Name: "Recebido", PipelineID: "funnel-a"},
			{Name: "Atendendo", PipelineID: "funnel-a"},
		},
	}
}

func enumOf(def tools.Definition) []string {
	return def.Parameters["target_tag_name"].Enum
}

func TestTheAISeesTheFunnelSomeoneMovedTheEntryInto(t *testing.T) {
	repo := movedEntryRepo()
	tool := &manageEntryStageTool{stageRepo: repo}

	def := tool.DefinitionWithContext(tools.ToolContext{
		WorkspaceID:  "ws-1",
		CampaignID:   "acc-1",
		CampaignType: "telegram",
		EntryID:      "conv-1",
	})

	enum := enumOf(def)
	if len(enum) != 2 || enum[0] != "Em negociacao" {
		t.Fatalf("enum = %v, want the funnel the entry actually sits in", enum)
	}
	if repo.askedPipeline != "funnel-b" {
		t.Fatalf("looked up pipeline %q, want funnel-b", repo.askedPipeline)
	}
	if repo.askedCampaign {
		t.Fatal("fell back to the channel funnel for an entry that is already staged")
	}
}

func TestAnUnstagedEntryStillUsesItsChannelFunnel(t *testing.T) {
	repo := movedEntryRepo()
	repo.entryStage = nil
	tool := &manageEntryStageTool{stageRepo: repo}

	def := tool.DefinitionWithContext(tools.ToolContext{
		WorkspaceID:  "ws-1",
		CampaignID:   "acc-1",
		CampaignType: "telegram",
		EntryID:      "conv-1",
	})

	if enum := enumOf(def); len(enum) != 2 || enum[0] != "Recebido" {
		t.Fatalf("enum = %v, want the channel's funnel for an unstaged entry", enum)
	}
	if !repo.askedCampaign {
		t.Fatal("an unstaged entry must fall back to its channel binding")
	}
}

func TestAStageWithNoFunnelFallsBackInsteadOfShowingNothing(t *testing.T) {
	repo := movedEntryRepo()
	repo.stageByID["stage-b1"] = &stage.Stage{ID: "stage-b1", Name: "Orfa", PipelineID: ""}
	tool := &manageEntryStageTool{stageRepo: repo}

	def := tool.DefinitionWithContext(tools.ToolContext{
		WorkspaceID:  "ws-1",
		CampaignID:   "acc-1",
		CampaignType: "telegram",
		EntryID:      "conv-1",
	})

	if enum := enumOf(def); len(enum) != 2 || enum[0] != "Recebido" {
		t.Fatalf("enum = %v, want the channel funnel when the current stage names none", enum)
	}
}

func TestWithoutAnEntryIDTheChannelFunnelIsUsed(t *testing.T) {
	repo := movedEntryRepo()
	tool := &manageEntryStageTool{stageRepo: repo}

	def := tool.DefinitionWithContext(tools.ToolContext{
		WorkspaceID:  "ws-1",
		CampaignID:   "acc-1",
		CampaignType: "telegram",
	})

	if enum := enumOf(def); len(enum) != 2 || enum[0] != "Recebido" {
		t.Fatalf("enum = %v, want the channel funnel when no entry is named", enum)
	}
	if repo.askedPipeline != "" {
		t.Fatalf("looked up pipeline %q without an entry to place", repo.askedPipeline)
	}
}
