package pipeline

type Repository interface {
	Create(p *Pipeline) error
	Update(p *Pipeline) error
	Delete(workspaceID, id string) error
	GetByID(workspaceID, id string) (*Pipeline, error)

	ListByWorkspace(workspaceID, objectType string) ([]*Pipeline, error)

	PromoteDefault(workspaceID, objectType, pipelineID string) error
}
