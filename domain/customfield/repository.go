package customfield

type Repository interface {
	Create(d *Definition) error
	Update(d *Definition) error
	Delete(workspaceID, id string) error
	GetByID(workspaceID, id string) (*Definition, error)
	ListByObject(workspaceID string, objectType ObjectType) ([]*Definition, error)
}

type KeyHistory interface {
	RetiredByKey(workspaceID string, objectType ObjectType, key string) ([]*Definition, error)
}

type Store interface {
	Repository
	KeyHistory
}
