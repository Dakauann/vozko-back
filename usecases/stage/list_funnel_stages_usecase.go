package stage_usecase

import (
	"errors"
	"strings"

	"vozko/domain/stage"
)

// ErrWorkspaceRequired refuses a funnel listing with no tenant. Answering it
// would mean reading every workspace's funnels.
var ErrWorkspaceRequired = errors.New("stage: workspace is required")

// Funnel is the little this listing needs to know about a conversation funnel.
//
// Declared here rather than imported from domain/pipeline, following the same
// rule pipelineStageSeeder already establishes in the composition root: stages
// and funnels are separate aggregates, the use case names the narrow port it
// needs, and the composition root is the only place that knows both.
type Funnel struct {
	ID        string
	Name      string
	IsDefault bool
	Position  int
}

// FunnelLister returns a workspace's conversation funnels, in the order the
// funnels page shows them.
type FunnelLister interface {
	ListConversationFunnels(workspaceID string) ([]Funnel, error)
}

// ListFunnelStagesUseCase returns every conversation stage in a workspace,
// grouped under the funnel it belongs to.
//
// It exists because the inbox filter could only ever offer ONE funnel's stages.
// ListStagesUseCase resolves a single funnel (the campaign's, or the workspace
// default), which is right for "what can this conversation be moved to" and
// wrong for "what can I filter the whole inbox by": a workspace with four
// funnels had three of them unreachable, and an agent filtering by a stage of
// the resolved funnel got an empty list while the conversations sat one funnel
// over. In production that was 17% of all staged conversations.
//
// Grouped on the server rather than in the browser so the funnel names and the
// stages come from one consistent read. Two separate fetches can disagree, and
// a stage rendered under the wrong funnel heading is worse than no heading.
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

	out := make([]stage.FunnelStages, 0, len(funnels)+1)
	claimed := make(map[string]struct{}, len(funnels))
	for _, f := range funnels {
		claimed[f.ID] = struct{}{}
		group := stage.FunnelStages{
			PipelineID:   f.ID,
			PipelineName: f.Name,
			IsDefault:    f.IsDefault,
			Position:     f.Position,
			// Never nil: a funnel with no columns yet must serialize as [] so a
			// client can render an empty group rather than crash on null.
			Stages: []*stage.Stage{},
		}
		if stages, ok := byPipeline[f.ID]; ok {
			group.Stages = sortedByPosition(stages)
		}
		out = append(out, group)
	}

	// Stages whose funnel is missing or unset still filter and are still
	// assigned to live conversations, so they ride a trailing group rather than
	// being dropped. Dropping them is how a filter quietly stops matching
	// conversations that are staged perfectly well.
	var orphans []*stage.Stage
	for pipelineID, stages := range byPipeline {
		if _, ok := claimed[pipelineID]; ok {
			continue
		}
		orphans = append(orphans, stages...)
	}
	if len(orphans) > 0 {
		out = append(out, stage.FunnelStages{
			PipelineID: "",
			Position:   len(funnels),
			Stages:     sortedByPosition(orphans),
		})
	}

	return out, nil
}

// sortedByPosition orders a funnel's columns the way the board draws them, with
// name as the tiebreak so the order is stable rather than whatever the map
// iteration produced.
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
