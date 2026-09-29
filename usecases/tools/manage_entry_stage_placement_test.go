package tools_usecase

import (
	"context"
	"fmt"
	"strings"
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
			"stage-b1": {ID: "stage-b1", WorkspaceID: "ws-1", Name: "Em negociacao", PipelineID: "funnel-b"},
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
	repo.stageByID["stage-b1"] = &stage.Stage{ID: "stage-b1", WorkspaceID: "ws-1", Name: "Orfa", PipelineID: ""}
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

func TestTheChannelAISeesTheConversationsOwnFunnel(t *testing.T) {
	repo := movedEntryRepo()
	tool := &manageEntryStageTool{stageRepo: repo}

	def := tool.DefinitionWithContext(tools.ToolContext{
		WorkspaceID: "ws-1",
		EntryID:     "conv-1",
		EntryType:   "unofficial_whatsapp",
	})

	if enum := enumOf(def); len(enum) != 2 || enum[1] != "Fechado" {
		t.Fatalf("enum = %v, want the stages of funnel-b", enum)
	}
	if repo.askedPipeline != "funnel-b" {
		t.Fatalf("looked up pipeline %q, want funnel-b", repo.askedPipeline)
	}
}

type recordingAssign struct{ moved []stage.AssignEntryStageInput }

func (r *recordingAssign) Execute(_ string, input stage.AssignEntryStageInput) (*stage.EntryStage, error) {
	r.moved = append(r.moved, input)
	return &stage.EntryStage{StageID: input.StageID}, nil
}

func (r *placementStageRepo) EnsureDefaultStagesForCampaign(string, string, string) error { return nil }

func movingTool() (*manageEntryStageTool, *recordingAssign) {
	repo := movedEntryRepo()
	repo.byPipeline["funnel-b"][1].ID = "stage-b2"
	assign := &recordingAssign{}
	return &manageEntryStageTool{stageRepo: repo, assignStage: assign}, assign
}

func moveTo(tool *manageEntryStageTool, name string) tools.ExecutionResult {
	result, _ := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__entry_id":      "conv-1",
		"__entry_type":    "telegram",
		"__workspace_id":  "ws-1",
		"__campaign_id":   "acc-1",
		"__campaign_type": "telegram",
	}, map[string]interface{}{"target_tag_name": name})
	return result
}

func TestTheAIMovesWithinTheFunnelTheConversationIsIn(t *testing.T) {
	tool, assign := movingTool()

	result := moveTo(tool, "Fechado")

	if result.IsError || len(assign.moved) != 1 || assign.moved[0].StageID != "stage-b2" {
		t.Fatalf("result = %+v, moved = %+v", result, assign.moved)
	}
}

func TestTheAICannotPickAStageOfTheCampaignFunnelTheConversationLeft(t *testing.T) {
	tool, assign := movingTool()

	result := moveTo(tool, "Recebido")

	if !result.IsError || len(assign.moved) != 0 || !strings.Contains(fmt.Sprint(result.Result), "Em negociacao") {
		t.Fatalf("result = %+v, moved = %+v", result, assign.moved)
	}
}
