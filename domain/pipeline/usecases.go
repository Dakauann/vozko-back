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
}

type UpdatePipelineInput struct {
	Name         *string `json:"name,omitempty"`
	DepartmentID *string `json:"departmentId,omitempty"`
	Position     *int    `json:"position,omitempty"`
	IsDefault    *bool   `json:"isDefault,omitempty"`
}
