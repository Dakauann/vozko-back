package container

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	pipeline_domain "vozko/domain/pipeline"
	stage_domain "vozko/domain/stage"
)

// pipelineStageSeeder fills a newly created conversation funnel with its first
// columns, either by duplicating another funnel or from the product defaults.
//
// It lives in the composition root because it bridges two aggregates: the
// pipeline use case declares the port (StageSeeder) and must not import stages,
// while the stage repository must not know funnels get created elsewhere. This
// is the only place allowed to know both.
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

	// One failed column must not abandon the rest: a funnel with three of four
	// stages is usable and repairable, a half-created one that aborted is not.
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
			// The first column is where new conversations land. Copying the source
			// funnel's own flag would be wrong when that funnel's initial stage sits
			// in the middle, so position decides it here.
			IsInitial: i == 0,
		}
		if err := s.stages.Create(st); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// stageSeed is the shape both sources reduce to, so the copy and default paths
// share one write loop instead of two nearly identical ones.
type stageSeed struct {
	Name        string
	Description string
	Color       string
}

func (s pipelineStageSeeder) resolveSeeds(workspaceID, copyFromPipelineID string, drawn []pipeline_domain.StageSeed) ([]stageSeed, error) {
	// The operator's own columns come first and are never mixed with a template.
	// Blank rows are dropped rather than rejected: an empty trailing row is what a
	// list editor produces when someone adds one and changes their mind, and
	// failing the whole creation over it would lose the funnel they did draw.
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
		// Duplicating an empty funnel would produce another empty one. The defaults
		// are the more useful answer, and the caller asked for a working funnel.
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

// defaultStageSeeds reads the product's canonical stages from the domain rather
// than restating them, so a change there reaches new funnels too.
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
