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

type GetPipelineUsageUseCase interface {
	Execute(workspaceID, id string) (Usage, error)
}

type DeletePipelineInput struct {
	MoveEntriesTo string `json:"moveEntriesTo,omitempty"`
}

type Occupancy interface {
	Usage(workspaceID, pipelineID string) (Usage, error)

	Vacate(workspaceID, pipelineID, intoPipelineID string) (moved int64, err error)
}

type ListPipelinesUseCase interface {
	Execute(workspaceID, objectType string) ([]*Pipeline, error)
}

type GetPipelineUseCase interface {
	Execute(workspaceID, id string) (*Pipeline, error)
}

type CreatePipelineInput struct {
	Name                     string `json:"name"`
	ObjectType               string `json:"objectType,omitempty"`
	DepartmentID             string `json:"departmentId,omitempty"`
	Position                 int    `json:"position,omitempty"`
	IsDefault                bool   `json:"isDefault,omitempty"`
	CopyStagesFromPipelineID string `json:"copyStagesFromPipelineId,omitempty"`

	Stages []StageSeed `json:"stages,omitempty"`
}

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
