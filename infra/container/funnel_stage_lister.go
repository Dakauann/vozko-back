package container

import (
	stagehttp "vozko/delivery/http/stage"
	pipeline_domain "vozko/domain/pipeline"
	stage_usecase "vozko/usecases/stage"
)

// conversationFunnelLister adapts the pipeline repository onto the narrow port
// the grouped stage listing declares.
//
// It lives in the composition root for the same reason pipelineStageSeeder
// does: funnels and stages are separate aggregates that must not import each
// other, so the use case names the little it needs and this is the only place
// allowed to know both sides. The filter on the far end wants exactly one
// thing, "which conversation funnels does this workspace have, in order", and
// that is all this hands over.
type conversationFunnelLister struct {
	pipelines pipeline_domain.Repository
}

func (l conversationFunnelLister) ListConversationFunnels(workspaceID string) ([]stage_usecase.Funnel, error) {
	// Conversation funnels only. A sales funnel's stages organize deals, and
	// offering them in the inbox filter would let an operator filter
	// conversations by a stage no conversation can ever hold.
	rows, err := l.pipelines.ListByWorkspace(workspaceID, string(pipeline_domain.ObjectConversation))
	if err != nil {
		return nil, err
	}
	out := make([]stage_usecase.Funnel, 0, len(rows))
	for _, p := range rows {
		if p == nil {
			continue
		}
		out = append(out, stage_usecase.Funnel{
			ID:        p.ID,
			Name:      p.Name,
			IsDefault: p.IsDefault,
			Position:  p.Position,
		})
	}
	return out, nil
}

// withFunnelStages attaches the grouped stage listing to the stage handler.
//
// A function rather than a twelfth positional argument to NewStageHandler,
// matching withLeadInboxSeeding: the listing is one collaborator the inbox
// filter needs, and a deployment without it still serves every other stage
// route.
func withFunnelStages(c *Container, h *stagehttp.StageHandler) *stagehttp.StageHandler {
	if c.repositories == nil || c.repositories.stage == nil || c.repositories.pipeline == nil {
		return h
	}
	h.SetFunnelStagesLister(stage_usecase.NewListFunnelStagesUseCase(
		c.repositories.stage,
		conversationFunnelLister{pipelines: c.repositories.pipeline},
	))
	return h
}
