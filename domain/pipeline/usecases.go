package pipeline

type CreatePipelineUseCase interface {
	Execute(workspaceID string, input CreatePipelineInput) (*Pipeline, error)
}

type UpdatePipelineUseCase interface {
	Execute(workspaceID, id string, input UpdatePipelineInput) (*Pipeline, error)
}

type DeletePipelineUseCase interface {
	Execute(workspaceID, id string, input DeletePipelineInput) error
}

// GetPipelineUsageUseCase answers "what does this funnel still hold" so the UI
// can state the consequence BEFORE the operator confirms, rather than letting
// them press delete and read a refusal.
type GetPipelineUsageUseCase interface {
	Execute(workspaceID, id string) (Usage, error)
}

type DeletePipelineInput struct {
	// MoveEntriesTo receives the conversations sitting on this funnel's stages.
	// Required when the funnel holds any, ignored when it holds none.
	MoveEntriesTo string `json:"moveEntriesTo,omitempty"`
}

// Occupancy reads what a funnel holds and empties it.
//
// It is a separate port from Repository because it spans aggregates the
// pipeline package must not import: conversation entries, campaigns, channel
// accounts and opportunities all name a pipeline, and none of them is the
// pipeline's to know. The composition root supplies the one adapter that may
// see them all.
type Occupancy interface {
	// Usage counts everything pointing at this funnel.
	Usage(workspaceID, pipelineID string) (Usage, error)

	// Vacate moves every conversation on this funnel's stages onto
	// intoPipelineID's entry stage, then removes the funnel's own stages, and
	// returns how many conversations moved.
	//
	// An empty intoPipelineID is valid ONLY for a funnel holding no
	// conversations, and then it just removes the columns. The caller has
	// already counted; this port does not re-decide.
	Vacate(workspaceID, pipelineID, intoPipelineID string) (moved int64, err error)
}

type ListPipelinesUseCase interface {
	Execute(workspaceID, objectType string) ([]*Pipeline, error)
}

type GetPipelineUseCase interface {
	Execute(workspaceID, id string) (*Pipeline, error)
}

type CreatePipelineInput struct {
	Name         string `json:"name"`
	ObjectType   string `json:"objectType,omitempty"`
	DepartmentID string `json:"departmentId,omitempty"`
	Position     int    `json:"position,omitempty"`
	IsDefault    bool   `json:"isDefault,omitempty"`
	// CopyStagesFromPipelineID duplicates an existing funnel's stages into the new
	// one. Empty seeds the product's default stages instead. Conversation funnels
	// only — a funnel with no stages renders an empty board nobody can add to.
	CopyStagesFromPipelineID string `json:"copyStagesFromPipelineId,omitempty"`

	// Stages are the columns the operator drew in the composer.
	//
	// When present they ARE the funnel, and CopyStagesFromPipelineID is ignored:
	// a named list is the more specific intent, the same way hand-picked bulk
	// targets beat a filter. This is the ordinary path now. Copying another funnel
	// and the product defaults become what they should always have been, a way to
	// PREFILL this list in the client, rather than a separate creation path that
	// produced a funnel the operator never actually chose.
	Stages []StageSeed `json:"stages,omitempty"`
}

// StageSeed is one column as the operator drew it: what it is called, what it
// means, and the colour it carries on the board. Position is the array's own
// order, and the first entry is where arriving conversations land.
type StageSeed struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color,omitempty"`
}

type UpdatePipelineInput struct {
	Name         *string `json:"name,omitempty"`
	DepartmentID *string `json:"departmentId,omitempty"`
	Position     *int    `json:"position,omitempty"`
	IsDefault    *bool   `json:"isDefault,omitempty"`
}
