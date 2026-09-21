package lead_memory

type ListQuery struct {
	Category *Category
	Limit    int
	Offset   int
}

const (
	DefaultListLimit = 50
	MaxListLimit     = 200
)

type Repository interface {
	Create(m *LeadMemory) error

	FindByID(workspaceID, id string) (*LeadMemory, error)

	FindByIDPrefix(workspaceID, leadID, prefix string) (*LeadMemory, error)

	ListByLead(workspaceID, leadID string, q ListQuery) ([]*LeadMemory, int64, error)

	CountByLead(workspaceID, leadID string) (int64, error)

	FindByNormalizedContent(workspaceID, leadID, contentNorm string) (*LeadMemory, error)

	Update(m *LeadMemory) error

	SoftDelete(workspaceID, id string) error
}
