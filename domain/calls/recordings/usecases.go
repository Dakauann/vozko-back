package recordings

import "vozko/domain/shared"

// QueryUseCase exposes read access to call recordings to the delivery layer, so
// handlers depend on an application-layer seam rather than the repository.
type QueryUseCase interface {
	GetByCallID(callID string) (*CallRecord, error)
	GetByLeadID(leadID string) ([]*CallRecord, error)
	GetByEntryID(entryID string) ([]*CallRecord, error)
	List(filters ListFilters) (*shared.PaginatedResult[*CallRecord], error)
}
