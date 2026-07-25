package label

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
	Execute(workspaceID, labelID, entryID, entryType string) error
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
}

type ReorderLabelsInput struct {
	LabelIDs []string `json:"labelIds"`
}
