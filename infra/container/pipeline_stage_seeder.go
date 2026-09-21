package container

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	pipeline_domain "vozko/domain/pipeline"
	stage_domain "vozko/domain/stage"
)

type pipelineStageSeeder struct {
	stages stage_domain.Repository
}

func (s pipelineStageSeeder) SeedConversationPipeline(workspaceID, pipelineID, copyFromPipelineID string, drawn []pipeline_domain.StageSeed) error {
	workspaceID = strings.TrimSpace(workspaceID)
	pipelineID = strings.TrimSpace(pipelineID)
	if workspaceID == "" || pipelineID == "" {
		return fmt.Errorf("seed funnel: workspace and pipeline are required")
	}

	seeds, err := s.resolveSeeds(workspaceID, strings.TrimSpace(copyFromPipelineID), drawn)
	if err != nil {
		return err
	}

	var firstErr error
	for i, seed := range seeds {
		st := &stage_domain.Stage{
			ID:          uuid.New().String(),
			WorkspaceID: workspaceID,
			PipelineID:  pipelineID,
			Name:        strings.ToLower(strings.TrimSpace(seed.Name)),
			Description: seed.Description,
			Color:       seed.Color,
			Position:    i + 1,
			IsInitial:   i == 0,
		}
		if err := s.stages.Create(st); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type stageSeed struct {
	Name        string
	Description string
	Color       string
}

func (s pipelineStageSeeder) resolveSeeds(workspaceID, copyFromPipelineID string, drawn []pipeline_domain.StageSeed) ([]stageSeed, error) {
	if len(drawn) > 0 {
		seeds := make([]stageSeed, 0, len(drawn))
		for _, d := range drawn {
			name := strings.TrimSpace(d.Name)
			if name == "" {
				continue
			}
			seeds = append(seeds, stageSeed{
				Name:        name,
				Description: strings.TrimSpace(d.Description),
				Color:       strings.TrimSpace(d.Color),
			})
		}
		if len(seeds) > 0 {
			return seeds, nil
		}
	}

	if copyFromPipelineID == "" {
		return defaultStageSeeds(), nil
	}

	source, err := s.stages.ListByPipeline(workspaceID, copyFromPipelineID)
	if err != nil {
		return nil, fmt.Errorf("read source funnel %s: %w", copyFromPipelineID, err)
	}
	if len(source) == 0 {
		return defaultStageSeeds(), nil
	}

	seeds := make([]stageSeed, 0, len(source))
	for _, st := range source {
		seeds = append(seeds, stageSeed{
			Name:        st.Name,
			Description: st.Description,
			Color:       st.Color,
		})
	}
	return seeds, nil
}

func defaultStageSeeds() []stageSeed {
	seeds := make([]stageSeed, 0, len(stage_domain.DefaultStages))
	for _, d := range stage_domain.DefaultStages {
		seeds = append(seeds, stageSeed{
			Name:        d.Name,
			Description: d.Description,
			Color:       d.Color,
		})
	}
	return seeds
}
