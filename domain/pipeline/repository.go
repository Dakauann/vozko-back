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

	// PromoteDefault makes this the workspace's one default funnel for its
	// object kind, demoting whatever held the flag before.
	//
	// One call rather than "clear the old one, then set the new one", because
	// those are two writes and a process that dies between them leaves the
	// workspace with no default at all — which the stage repository then
	// self-heals by creating yet another funnel. The implementation does both
	// sides in a single transaction.
	//
	// Scoped to the object kind: a workspace legitimately has one default
	// conversation funnel and one default sales funnel at the same time.
	PromoteDefault(workspaceID, objectType, pipelineID string) error
}
