package container

import (
	stagehttp "vozko/delivery/http/stage"
	pipeline_domain "vozko/domain/pipeline"
	stage_usecase "vozko/usecases/stage"
)

type conversationFunnelLister struct {
	pipelines pipeline_domain.Repository
}

func (l conversationFunnelLister) ListConversationFunnels(workspaceID string) ([]stage_usecase.Funnel, error) {
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
