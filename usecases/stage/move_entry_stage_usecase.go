package stage_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/stage"
)

type moveEntryStage struct {
	access shared.EntryAccessChecker
	assign stage.AssignEntryStageUseCase
}

func NewMoveEntryStageUseCase(access shared.EntryAccessChecker, assign stage.AssignEntryStageUseCase) stage.MoveEntryStageUseCase {
	return &moveEntryStage{access: access, assign: assign}
}

func (m *moveEntryStage) Execute(workspaceID string, by shared.Person, input stage.AssignEntryStageInput) (*stage.EntryStage, error) {
	if !by.MayActOn(m.access, workspaceID, input.EntryID, input.EntryType) {
		return nil, stage.ErrEntryAccess
	}
	input.ActorID = by.UserID
	return m.assign.Execute(workspaceID, input)
}
