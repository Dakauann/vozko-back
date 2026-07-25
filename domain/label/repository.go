package label

type Repository interface {
	Create(label *Label) error
	Update(label *Label) error
	Delete(id string) error
	FindByID(id string) (*Label, error)
	ListByWorkspace(workspaceID string) ([]*Label, error)
	NameExistsInWorkspace(workspaceID, name string, excludeID *string) (bool, error)

	ReorderLabels(workspaceID string, labelIDs []string) error

	AssignLabel(entryLabel *EntryLabel) error
	RemoveLabel(labelID, entryID, entryType, workspaceID string) error
	RemoveEntryLabels(entryID, entryType, workspaceID string) error
	GetEntryLabels(entryID, entryType, workspaceID string) ([]*EntryLabel, error)
	GetBatchEntryLabels(entryIDs []string, entryType, workspaceID string) (map[string][]*EntryLabel, error)
	GetEntriesByLabel(labelID, workspaceID string) ([]*EntryLabel, error)
}

type LabelGroupRepository interface {
	Create(group *LabelGroup) error
	Update(group *LabelGroup) error
	Delete(id string) error
	FindByID(id string) (*LabelGroup, error)
	ListByWorkspace(workspaceID string) ([]*LabelGroup, error)
	AddItem(item *LabelGroupItem) error
	RemoveItem(itemID string) error
	GetItems(labelGroupID string) ([]LabelGroupItem, error)
}
