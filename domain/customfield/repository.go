package customfield

type Repository interface {
	Create(d *Definition) error
	Update(d *Definition) error
	Delete(workspaceID, id string) error
	GetByID(workspaceID, id string) (*Definition, error)
	ListByObject(workspaceID, objectType string) ([]*Definition, error)
}
