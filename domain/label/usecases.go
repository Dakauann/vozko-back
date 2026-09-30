package label

import (
	"strings"

	"vozko/domain/shared"
)

type CreateLabelUseCase interface {
	Execute(workspaceID string, input CreateLabelInput) (*Label, error)
}

type UpdateLabelUseCase interface {
	Execute(workspaceID, labelID string, input UpdateLabelInput) (*Label, error)
}

type DeleteLabelUseCase interface {
	Execute(workspaceID, labelID string) error
}

type ListLabelsUseCase interface {
	Execute(workspaceID string) ([]*Label, error)
}

type AssignEntryLabelUseCase interface {
	Execute(workspaceID string, input AssignEntryLabelInput) (*EntryLabel, error)
}

type RemoveEntryLabelUseCase interface {
	Execute(workspaceID string, input RemoveEntryLabelInput) error
}

type GetEntryLabelsUseCase interface {
	Execute(workspaceID, entryID, entryType string) ([]*EntryLabel, error)
}

type ReorderLabelsUseCase interface {
	Execute(workspaceID string, input ReorderLabelsInput) ([]*Label, error)
}

type CreateLabelInput struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

type UpdateLabelInput struct {
	Name  *string `json:"name,omitempty"`
	Color *string `json:"color,omitempty"`
}

type AssignEntryLabelInput struct {
	LabelID   string `json:"labelId"`
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`
	ActorID   string `json:"-"`
}

type RemoveEntryLabelInput struct {
	LabelID   string `json:"labelId"`
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`
	ActorID   string `json:"-"`
}

type ReorderLabelsInput struct {
	LabelIDs []string `json:"labelIds"`
}

type EntryLabelsUseCase interface {
	Apply(workspaceID string, by shared.Person, input AssignEntryLabelInput) (*EntryLabel, error)
	Remove(workspaceID string, by shared.Person, input RemoveEntryLabelInput) error
}

type LabelAction string

const (
	LabelActionAdd    LabelAction = "add"
	LabelActionRemove LabelAction = "remove"
)

func LabelActions() []string {
	return []string{string(LabelActionAdd), string(LabelActionRemove)}
}

func ParseLabelAction(raw string) (LabelAction, error) {
	switch action := LabelAction(strings.ToLower(strings.TrimSpace(raw))); action {
	case "", LabelActionAdd:
		return LabelActionAdd, nil
	case LabelActionRemove:
		return LabelActionRemove, nil
	}
	return "", ErrInvalidLabelAction
}

type LabelChangeRequest struct {
	Action    LabelAction
	LabelID   string
	EntryID   string
	EntryType string
	ActorID   string
}

type LabelChange struct {
	LabelID   string
	LabelName string
	Unchanged bool
}

type AutomationLabeler interface {
	Change(workspaceID string, request LabelChangeRequest) (LabelChange, error)
}

type UpdateBroadcaster interface {
	BroadcastLabelUpdate(workspaceID, entryID, entryType string)
}
