package pipeline

type CreatePipelineUseCase interface {
	Execute(workspaceID string, input CreatePipelineInput) (*Pipeline, error)
}

type UpdatePipelineUseCase interface {
	Execute(workspaceID, id string, input UpdatePipelineInput) (*Pipeline, error)
}

type DeletePipelineUseCase interface {
	Execute(workspaceID, id string) error
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
}

type UpdatePipelineInput struct {
	Name         *string `json:"name,omitempty"`
	DepartmentID *string `json:"departmentId,omitempty"`
	Position     *int    `json:"position,omitempty"`
	IsDefault    *bool   `json:"isDefault,omitempty"`
}
