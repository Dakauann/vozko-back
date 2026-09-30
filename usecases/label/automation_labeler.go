package label_usecase

import (
	"errors"

	"vozko/domain/label"
)

type automationLabeler struct {
	assign  label.AssignEntryLabelUseCase
	remove  label.RemoveEntryLabelUseCase
	current label.GetEntryLabelsUseCase
	screens label.UpdateBroadcaster
}

func NewAutomationLabeler(
	assign label.AssignEntryLabelUseCase,
	remove label.RemoveEntryLabelUseCase,
	current label.GetEntryLabelsUseCase,
	screens label.UpdateBroadcaster,
) label.AutomationLabeler {
	return &automationLabeler{assign: assign, remove: remove, current: current, screens: screens}
}

func (l *automationLabeler) Change(workspaceID string, request label.LabelChangeRequest) (label.LabelChange, error) {
	switch request.Action {
	case label.LabelActionAdd:
		return l.add(workspaceID, request)
	case label.LabelActionRemove:
		return l.take(workspaceID, request)
	}
	return label.LabelChange{}, label.ErrInvalidLabelAction
}

func (l *automationLabeler) add(workspaceID string, request label.LabelChangeRequest) (label.LabelChange, error) {
	assigned, err := l.assign.Execute(workspaceID, label.AssignEntryLabelInput{
		LabelID:   request.LabelID,
		EntryID:   request.EntryID,
		EntryType: request.EntryType,
		ActorID:   request.ActorID,
	})
	if errors.Is(err, label.ErrEntryLabelExists) {
		current, found, err := l.onConversation(workspaceID, request)
		if err != nil || !found {
			return label.LabelChange{}, errors.Join(err, label.ErrEntryLabelNotFound)
		}
		return label.LabelChange{LabelID: current.LabelID, LabelName: current.LabelName, Unchanged: true}, nil
	}
	if err != nil {
		return label.LabelChange{}, err
	}
	l.screens.BroadcastLabelUpdate(workspaceID, request.EntryID, request.EntryType)
	return label.LabelChange{LabelID: assigned.LabelID, LabelName: assigned.LabelName}, nil
}

func (l *automationLabeler) take(workspaceID string, request label.LabelChangeRequest) (label.LabelChange, error) {
	current, found, err := l.onConversation(workspaceID, request)
	if err != nil {
		return label.LabelChange{}, err
	}
	if !found {
		return label.LabelChange{LabelID: request.LabelID, Unchanged: true}, nil
	}
	if err := l.remove.Execute(workspaceID, label.RemoveEntryLabelInput{
		LabelID:   request.LabelID,
		EntryID:   request.EntryID,
		EntryType: request.EntryType,
		ActorID:   request.ActorID,
	}); err != nil {
		return label.LabelChange{}, err
	}
	l.screens.BroadcastLabelUpdate(workspaceID, request.EntryID, request.EntryType)
	return label.LabelChange{LabelID: current.LabelID, LabelName: current.LabelName}, nil
}

func (l *automationLabeler) onConversation(workspaceID string, request label.LabelChangeRequest) (*label.EntryLabel, bool, error) {
	labels, err := l.current.Execute(workspaceID, request.EntryID, request.EntryType)
	if err != nil {
		return nil, false, err
	}
	for _, existing := range labels {
		if existing != nil && existing.LabelID == request.LabelID {
			return existing, true, nil
		}
	}
	return nil, false, nil
}
