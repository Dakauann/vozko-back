package stage_usecase

import (
	"errors"
	"strings"

	"vozko/domain/stage"
)

var ErrWorkspaceRequired = errors.New("stage: workspace is required")

type Funnel struct {
	ID        string
	Name      string
	IsDefault bool
	Position  int
}

type FunnelLister interface {
	ListConversationFunnels(workspaceID string) ([]Funnel, error)
}

type ListFunnelStagesUseCase struct {
	stages  stage.Repository
	funnels FunnelLister
}

func NewListFunnelStagesUseCase(stages stage.Repository, funnels FunnelLister) *ListFunnelStagesUseCase {
	return &ListFunnelStagesUseCase{stages: stages, funnels: funnels}
}

func (uc *ListFunnelStagesUseCase) Execute(workspaceID string) ([]stage.FunnelStages, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, ErrWorkspaceRequired
	}

	funnels, err := uc.funnels.ListConversationFunnels(workspaceID)
	if err != nil {
		return nil, err
	}
	all, err := uc.stages.ListByWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	byPipeline := make(map[string][]*stage.Stage, len(funnels))
	for _, st := range all {
		if st == nil {
			continue
		}
		byPipeline[strings.TrimSpace(st.PipelineID)] = append(byPipeline[strings.TrimSpace(st.PipelineID)], st)
	}

	out := make([]stage.FunnelStages, 0, len(funnels))
	for _, f := range funnels {
		group := stage.FunnelStages{
			PipelineID:   f.ID,
			PipelineName: f.Name,
			IsDefault:    f.IsDefault,
			Position:     f.Position,
			Stages:       []*stage.Stage{},
		}
		if stages, ok := byPipeline[f.ID]; ok {
			group.Stages = sortedByPosition(stages)
		}
		out = append(out, group)
	}

	return out, nil
}

func sortedByPosition(stages []*stage.Stage) []*stage.Stage {
	out := append([]*stage.Stage(nil), stages...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			if less(out[j], out[j-1]) {
				out[j], out[j-1] = out[j-1], out[j]
				continue
			}
			break
		}
	}
	return out
}

func less(a, b *stage.Stage) bool {
	if a.Position != b.Position {
		return a.Position < b.Position
	}
	return a.Name < b.Name
}
