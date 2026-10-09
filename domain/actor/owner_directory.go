package actor

type OwnerDirectory interface {
	Belongs(workspaceID, actorID string) (bool, error)
}
