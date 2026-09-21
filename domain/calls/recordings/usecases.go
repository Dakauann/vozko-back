package recordings

import "vozko/domain/shared"

type QueryUseCase interface {
	GetByCallID(callID string) (*CallRecord, error)
	GetByLeadID(leadID string) ([]*CallRecord, error)
	GetByEntryID(entryID string) ([]*CallRecord, error)
	List(filters ListFilters) (*shared.PaginatedResult[*CallRecord], error)
}
