package lead_memory

// ListQuery filters and pages a per-lead listing. Zero values mean "no filter,
// first page, default size".
type ListQuery struct {
	Category *Category
	Limit    int // default DefaultListLimit, capped at MaxListLimit
	Offset   int
}

const (
	DefaultListLimit = 50
	MaxListLimit     = 200
)

// Repository persists lead memories. Every read and write is workspace-scoped
// by argument; a row from another workspace behaves exactly like a missing row
// (ErrNotFound), never like a permission error.
type Repository interface {
	// Create inserts the row. A unique violation on the dedup key translates
	// to ErrDuplicate so the use case can resolve the race by re-reading.
	Create(m *LeadMemory) error

	FindByID(workspaceID, id string) (*LeadMemory, error)

	// FindByIDPrefix resolves the short ids rendered in the prompt block.
	// Zero matches → ErrNotFound; more than one → ErrAmbiguousID. Scoped to
	// the lead because prefixes are only meaningful within one lead's block.
	FindByIDPrefix(workspaceID, leadID, prefix string) (*LeadMemory, error)

	// ListByLead returns memories newest-first plus the unpaged total.
	ListByLead(workspaceID, leadID string, q ListQuery) ([]*LeadMemory, int64, error)

	CountByLead(workspaceID, leadID string) (int64, error)

	// FindByNormalizedContent is the deterministic dedup probe: the active
	// memory whose NormalizeContent(content) equals contentNorm, or ErrNotFound.
	FindByNormalizedContent(workspaceID, leadID, contentNorm string) (*LeadMemory, error)

	Update(m *LeadMemory) error

	// SoftDelete hides the row from every read path while preserving it for
	// audit. Hard deletion rides the lead's own lifecycle (FK cascade).
	SoftDelete(workspaceID, id string) error
}
