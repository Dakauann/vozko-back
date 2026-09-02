package label_usecase

import (
	ce "vozko/domain/conversation_event"
	"vozko/domain/label"
	"vozko/domain/shared"
)

type removeEntryLabelUseCase struct {
	repo   label.Repository
	events ce.Logger
}

// NewRemoveEntryLabelUseCase wires the label removal.
//
// events may be nil (unit tests). Held by the USE CASE rather than the HTTP
// handler that used to hold it, so the CRM's bulk "remove label" records the
// change on every conversation it touches.
func NewRemoveEntryLabelUseCase(repo label.Repository, events ce.Logger) label.RemoveEntryLabelUseCase {
	return &removeEntryLabelUseCase{repo: repo, events: events}
}

func (uc *removeEntryLabelUseCase) Execute(workspaceID string, input label.RemoveEntryLabelInput) error {
	if err := label.ValidateEntryType(input.EntryType); err != nil {
		return err
	}

	l, err := uc.repo.FindByID(input.LabelID)
	if err != nil {
		return err
	}
	if l.WorkspaceID != workspaceID {
		return label.ErrUnauthorized
	}

	if err := uc.repo.RemoveLabel(input.LabelID, input.EntryID, input.EntryType, workspaceID); err != nil {
		return err
	}

	if uc.events != nil {
		uc.events.Log(ce.New(workspaceID, input.EntryID, input.EntryType, ce.EventLabelRemoved).
			WithActor(input.ActorID).
			WithChannel(shared.EntryType(input.EntryType).EventChannel()).
			WithDetails(map[string]string{"label_id": input.LabelID, "label_name": l.Name}).
			Build())
	}
	return nil
}
