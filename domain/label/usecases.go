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

// AssignEntryLabelInput adds one label to an entry.
//
// ActorID names who is doing it, in the stored actor-id form: a user uuid, an
// "ai:<agentID>" attendant, or "system"/empty for the platform. It is on the
// INPUT because the use case writes the timeline event; while that write lived
// in the HTTP handler, every other caller (bulk, the AI) left no trace.
type AssignEntryLabelInput struct {
	LabelID   string `json:"labelId"`
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`
	ActorID   string `json:"-"`
}

// RemoveEntryLabelInput takes a label off an entry. See AssignEntryLabelInput
// for ActorID; a struct for the same reason, so a new field cannot be silently
// dropped by a caller passing positional strings.
type RemoveEntryLabelInput struct {
	LabelID   string `json:"labelId"`
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`
	ActorID   string `json:"-"`
}

type ReorderLabelsInput struct {
	LabelIDs []string `json:"labelIds"`
}
