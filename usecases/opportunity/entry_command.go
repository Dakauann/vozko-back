package opportunity_usecase

import (
	"errors"
	"time"

	"vozko/domain/opportunity"
	"vozko/domain/stage"
)

var (
	ErrNoOpenDeal    = errors.New("opportunity: this conversation has no open deal")
	ErrUnknownAction = errors.New("opportunity: unknown action")
)

type EntryAction string

const (
	EntryCreate      EntryAction = "create"
	EntryUpdateValue EntryAction = "update_value"
	EntryMove        EntryAction = "move"
	EntryWin         EntryAction = "win"
	EntryLose        EntryAction = "lose"
)

func EntryActions() []EntryAction {
	return []EntryAction{EntryCreate, EntryUpdateValue, EntryMove, EntryWin, EntryLose}
}

func (a EntryAction) Valid() bool {
	for _, known := range EntryActions() {
		if a == known {
			return true
		}
	}
	return false
}

type EntryCommand struct {
	EntryID    string
	EntryType  string
	LeadID     string
	PipelineID string
	Actor      string

	Action       EntryAction
	Title        string
	ValueCents   *int64
	Currency     string
	StageID      string
	LostReasonID string
}

type EntryResult struct {
	Opportunity *opportunity.Opportunity
	Created     bool
}

func (s *Service) ManageForEntry(workspaceID string, cmd EntryCommand) (*EntryResult, error) {
	if cmd.Actor == "" {
		return nil, ErrActorRequired
	}
	if cmd.EntryID == "" || cmd.EntryType == "" {
		return nil, ErrEntryTypeRequired
	}
	if !cmd.Action.Valid() {
		return nil, ErrUnknownAction
	}
	stages, err := s.PipelineStages(workspaceID, cmd.PipelineID)
	if err != nil {
		return nil, err
	}
	target, err := s.targetStage(workspaceID, cmd, stages)
	if err != nil {
		return nil, err
	}

	var result EntryResult
	err = s.repo.WithEntryLock(workspaceID, cmd.EntryID, cmd.EntryType, func(store opportunity.Store) error {
		current, err := store.OpenForEntry(workspaceID, cmd.PipelineID, cmd.EntryID, cmd.EntryType)
		switch {
		case errors.Is(err, opportunity.ErrNotFound):
			if cmd.Action == EntryLose {
				return ErrNoOpenDeal
			}
			created, err := s.createForEntry(workspaceID, cmd, stages, target, store)
			result = EntryResult{Opportunity: created, Created: true}
			return err
		case err != nil:
			return err
		}
		updated, err := s.updateForEntry(cmd, current, target, store)
		result = EntryResult{Opportunity: updated}
		return err
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) targetStage(workspaceID string, cmd EntryCommand, stages []*stage.Stage) (*opportunity.StageRef, error) {
	switch cmd.Action {
	case EntryWin, EntryLose:
		ref, err := closingStage(stages, cmd.Action == EntryWin)
		if err != nil {
			return nil, err
		}
		return &ref, nil
	case EntryMove:
		if cmd.StageID == "" {
			return nil, opportunity.ErrStageRequired
		}
	}
	if cmd.StageID == "" {
		return nil, nil
	}
	ref, err := s.placement(workspaceID, cmd.PipelineID, cmd.StageID)
	if err != nil {
		return nil, err
	}
	if ref.PipelineID != cmd.PipelineID {
		return nil, opportunity.ErrStageOutsidePipeline
	}
	return &ref, nil
}

func (s *Service) createForEntry(
	workspaceID string,
	cmd EntryCommand,
	stages []*stage.Stage,
	target *opportunity.StageRef,
	store opportunity.Store,
) (*opportunity.Opportunity, error) {
	initial, err := initialStage(stages)
	if err != nil {
		return nil, err
	}
	o, links, events, err := s.prepareCreate(workspaceID, CreateInput{
		LeadID:        cmd.LeadID,
		PipelineID:    cmd.PipelineID,
		StageID:       initial.ID,
		Title:         cmd.Title,
		Currency:      cmd.Currency,
		LinkEntryID:   cmd.EntryID,
		LinkEntryType: cmd.EntryType,
		Actor:         cmd.Actor,
	})
	if err != nil {
		return nil, err
	}
	placed := *o
	now := s.now()
	if err := applyEntryCommand(&placed, cmd, target, now); err != nil {
		return nil, err
	}
	events = append(events, opportunity.Changes(o, &placed, cmd.Actor, now)...)
	if err := store.Create(&placed, links, events); err != nil {
		return nil, err
	}
	return &placed, nil
}

func (s *Service) updateForEntry(
	cmd EntryCommand,
	current *opportunity.Opportunity,
	target *opportunity.StageRef,
	store opportunity.Store,
) (*opportunity.Opportunity, error) {
	before := *current
	now := s.now()
	if err := applyEntryCommand(current, cmd, target, now); err != nil {
		return nil, err
	}
	if err := store.Update(current, opportunity.Changes(&before, current, cmd.Actor, now)); err != nil {
		return nil, err
	}
	return current, nil
}

func applyEntryCommand(o *opportunity.Opportunity, cmd EntryCommand, target *opportunity.StageRef, now time.Time) error {
	if cmd.Title != "" {
		o.Title = cmd.Title
	}
	if cmd.ValueCents != nil {
		o.ValueCents = *cmd.ValueCents
	}
	if cmd.Currency != "" {
		o.Currency = cmd.Currency
	}
	if cmd.LostReasonID != "" {
		o.LostReasonID = cmd.LostReasonID
	}
	if target != nil {
		if err := o.PlaceOn(*target, cmd.Actor, now); err != nil {
			return err
		}
	}
	o.Normalize()
	return o.Validate()
}

func (s *Service) CurrentDealForEntry(workspaceID, pipelineID, entryID, entryType string) (*opportunity.Opportunity, error) {
	if err := s.dealPipeline(workspaceID, pipelineID); err != nil {
		return nil, err
	}
	if entryID == "" || entryType == "" {
		return nil, ErrEntryTypeRequired
	}
	return s.repo.CurrentForEntry(workspaceID, pipelineID, entryID, entryType)
}
