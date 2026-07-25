package calls_query_usecase

import (
	"errors"
	"testing"

	"vozko/domain/calls/recordings"
	"vozko/domain/shared"
)

type recordingRepoStub struct {
	recordings.CallRecordRepository
	gotCallID  string
	gotLeadID  string
	gotEntryID string
	gotFilters recordings.ListFilters
	one        *recordings.CallRecord
	many       []*recordings.CallRecord
	page       *shared.PaginatedResult[*recordings.CallRecord]
	err        error
}

func (s *recordingRepoStub) GetByCallID(id string) (*recordings.CallRecord, error) {
	s.gotCallID = id
	return s.one, s.err
}
func (s *recordingRepoStub) GetByLeadID(id string) ([]*recordings.CallRecord, error) {
	s.gotLeadID = id
	return s.many, s.err
}
func (s *recordingRepoStub) GetByEntryID(id string) ([]*recordings.CallRecord, error) {
	s.gotEntryID = id
	return s.many, s.err
}
func (s *recordingRepoStub) List(f recordings.ListFilters) (*shared.PaginatedResult[*recordings.CallRecord], error) {
	s.gotFilters = f
	return s.page, s.err
}

func TestRecordingQueryDelegatesEveryMethod(t *testing.T) {
	repo := &recordingRepoStub{
		one:  &recordings.CallRecord{CallID: "c1"},
		many: []*recordings.CallRecord{{CallID: "c1"}},
		page: &shared.PaginatedResult[*recordings.CallRecord]{TotalItems: 1},
	}
	uc := NewRecordingQueryUseCase(repo)

	if r, err := uc.GetByCallID("c1"); err != nil || repo.gotCallID != "c1" || r == nil {
		t.Errorf("GetByCallID delegation failed")
	}
	if _, err := uc.GetByLeadID("l1"); err != nil || repo.gotLeadID != "l1" {
		t.Errorf("GetByLeadID delegation failed")
	}
	if _, err := uc.GetByEntryID("e1"); err != nil || repo.gotEntryID != "e1" {
		t.Errorf("GetByEntryID delegation failed")
	}
	if p, err := uc.List(recordings.ListFilters{LeadID: "l1"}); err != nil || repo.gotFilters.LeadID != "l1" || p == nil {
		t.Errorf("List delegation failed")
	}
}

func TestRecordingQueryPropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	uc := NewRecordingQueryUseCase(&recordingRepoStub{err: sentinel})
	if _, err := uc.GetByCallID("c1"); !errors.Is(err, sentinel) {
		t.Fatalf("expected error to propagate, got %v", err)
	}
}
