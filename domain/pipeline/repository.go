package pipeline

// Repository is the persistence port for pipelines. It lives in the domain so
// usecases depend on this interface, not on the GORM implementation in infra.
// Everything is workspace-scoped (multi-tenant).
type Repository interface {
	Create(p *Pipeline) error
	Update(p *Pipeline) error
	Delete(workspaceID, id string) error
	GetByID(workspaceID, id string) (*Pipeline, error)

	// ListByWorkspace returns the workspace's pipelines, ordered by Position.
	// When objectType is non-empty it is filtered to that object kind.
	ListByWorkspace(workspaceID, objectType string) ([]*Pipeline, error)
}
