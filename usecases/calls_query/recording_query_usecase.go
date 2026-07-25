package calls_query_usecase

import (
	"vozko/domain/calls/recordings"
	"vozko/domain/shared"
)

type recordingQueryUseCase struct {
	repo recordings.CallRecordRepository
}

func NewRecordingQueryUseCase(repo recordings.CallRecordRepository) recordings.QueryUseCase {
	return &recordingQueryUseCase{repo: repo}
}

func (uc *recordingQueryUseCase) GetByCallID(callID string) (*recordings.CallRecord, error) {
	return uc.repo.GetByCallID(callID)
}

func (uc *recordingQueryUseCase) GetByLeadID(leadID string) ([]*recordings.CallRecord, error) {
	return uc.repo.GetByLeadID(leadID)
}

func (uc *recordingQueryUseCase) GetByEntryID(entryID string) ([]*recordings.CallRecord, error) {
	return uc.repo.GetByEntryID(entryID)
}

func (uc *recordingQueryUseCase) List(filters recordings.ListFilters) (*shared.PaginatedResult[*recordings.CallRecord], error) {
	return uc.repo.List(filters)
}
