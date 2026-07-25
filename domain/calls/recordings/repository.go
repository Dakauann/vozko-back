package recordings

import "vozko/domain/shared"

type CallRecordRepository interface {
	Save(record *CallRecord) error
	GetByCallID(callID string) (*CallRecord, error)
	GetByLeadID(leadID string) ([]*CallRecord, error)
	GetByEntryID(entryID string) ([]*CallRecord, error)
	Update(record *CallRecord) error
	List(filters ListFilters) (*shared.PaginatedResult[*CallRecord], error)
}

type ListFilters struct {
	Pagination  shared.Pagination
	LeadID      string
	EntryID     string
	MinDuration *int
	MaxDuration *int
	SortBy      string
	SortDesc    bool
}
