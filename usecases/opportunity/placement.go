package opportunity_usecase

import (
	"errors"
	"sort"
	"strings"
	"time"

	"vozko/domain/opportunity"
	"vozko/domain/pipeline"
	"vozko/domain/stage"
)

var (
	ErrPipelineHasNoOpenStage = errors.New("opportunity: the pipeline has no open stage")
	ErrPipelineHasNoWonStage  = errors.New("opportunity: the pipeline has no won stage")
	ErrPipelineHasNoLostStage = errors.New("opportunity: the pipeline has no lost stage")
)

type StageReader interface {
	FindByID(id string) (*stage.Stage, error)
	ListByPipeline(workspaceID, pipelineID string) ([]*stage.Stage, error)
}

type PipelineReader interface {
	GetByID(workspaceID, id string) (*pipeline.Pipeline, error)
	ListByWorkspace(workspaceID, objectType string) ([]*pipeline.Pipeline, error)
}

func (s *Service) DealPipelines(workspaceID string) ([]*pipeline.Pipeline, error) {
	return s.pipelines.ListByWorkspace(workspaceID, string(pipeline.ObjectOpportunity))
}

func (s *Service) dealPipeline(workspaceID, pipelineID string) error {
	if strings.TrimSpace(pipelineID) == "" {
		return opportunity.ErrPipelineRequired
	}
	p, err := s.pipelines.GetByID(workspaceID, pipelineID)
	if errors.Is(err, pipeline.ErrNotFound) {
		return ErrPipelineNotFound
	}
	if err != nil {
		return err
	}
	if p.ObjectType != pipeline.ObjectOpportunity {
		return ErrNotOpportunityPipeline
	}
	return nil
}

func (s *Service) placement(workspaceID, pipelineID, stageID string) (opportunity.StageRef, error) {
	if err := s.dealPipeline(workspaceID, pipelineID); err != nil {
		return opportunity.StageRef{}, err
	}
	if strings.TrimSpace(stageID) == "" {
		return opportunity.StageRef{}, opportunity.ErrStageRequired
	}
	st, err := s.stages.FindByID(stageID)
	if errors.Is(err, stage.ErrTagNotFound) {
		return opportunity.StageRef{}, ErrStageNotFound
	}
	if err != nil {
		return opportunity.StageRef{}, err
	}
	if st.WorkspaceID != workspaceID {
		return opportunity.StageRef{}, ErrStageNotFound
	}
	return stageRef(st), nil
}

func (s *Service) moveTo(workspaceID string, o *opportunity.Opportunity, stageID, actorID string, now time.Time) error {
	ref, err := s.placement(workspaceID, o.PipelineID, stageID)
	if err != nil {
		return err
	}
	return o.PlaceOn(ref, actorID, now)
}

func (s *Service) PipelineStages(workspaceID, pipelineID string) ([]*stage.Stage, error) {
	if err := s.dealPipeline(workspaceID, pipelineID); err != nil {
		return nil, err
	}
	stages, err := s.stages.ListByPipeline(workspaceID, pipelineID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(stages, func(i, j int) bool { return stages[i].Position < stages[j].Position })
	return stages, nil
}

func (s *Service) checkOwner(workspaceID, ownerID string) error {
	if s.owners == nil {
		return ErrOwnerDirectoryMissing
	}
	belongs, err := s.owners.Belongs(workspaceID, ownerID)
	if err != nil {
		return err
	}
	if !belongs {
		return ErrOwnerOutsideWorkspace
	}
	return nil
}

func stageRef(st *stage.Stage) opportunity.StageRef {
	return opportunity.StageRef{ID: st.ID, PipelineID: st.PipelineID, IsWon: st.IsWon, IsLost: st.IsLost}
}

func initialStage(stages []*stage.Stage) (opportunity.StageRef, error) {
	for _, st := range stages {
		if st.IsInitial && !st.IsWon && !st.IsLost {
			return stageRef(st), nil
		}
	}
	for _, st := range stages {
		if !st.IsWon && !st.IsLost {
			return stageRef(st), nil
		}
	}
	return opportunity.StageRef{}, ErrPipelineHasNoOpenStage
}

func closingStage(stages []*stage.Stage, won bool) (opportunity.StageRef, error) {
	for _, st := range stages {
		if (won && st.IsWon) || (!won && st.IsLost) {
			return stageRef(st), nil
		}
	}
	if won {
		return opportunity.StageRef{}, ErrPipelineHasNoWonStage
	}
	return opportunity.StageRef{}, ErrPipelineHasNoLostStage
}
