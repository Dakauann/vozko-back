package calls_query_usecase

import (
	"errors"
	"testing"

	"vozko/domain/calls/billing"
)

type billingRepoStub struct {
	billing.Repository
	gotCallID  string
	gotCallIDs []string
	record     *billing.CallBillingRecord
	byID       map[string]*billing.CallBillingRecord
	err        error
}

func (s *billingRepoStub) GetByCallID(callID string) (*billing.CallBillingRecord, error) {
	s.gotCallID = callID
	return s.record, s.err
}

func (s *billingRepoStub) GetByCallIDs(callIDs []string) (map[string]*billing.CallBillingRecord, error) {
	s.gotCallIDs = callIDs
	return s.byID, s.err
}

func TestBillingQueryGetByCallIDDelegates(t *testing.T) {
	repo := &billingRepoStub{record: &billing.CallBillingRecord{CallID: "c1"}}
	uc := NewBillingQueryUseCase(repo)

	rec, err := uc.GetByCallID("c1")
	if err != nil || repo.gotCallID != "c1" || rec == nil || rec.CallID != "c1" {
		t.Fatalf("GetByCallID delegation failed: rec=%v got=%q err=%v", rec, repo.gotCallID, err)
	}
}

func TestBillingQueryGetByCallIDsDelegates(t *testing.T) {
	want := map[string]*billing.CallBillingRecord{"c1": {CallID: "c1"}}
	repo := &billingRepoStub{byID: want}
	uc := NewBillingQueryUseCase(repo)

	got, err := uc.GetByCallIDs([]string{"c1", "c2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.gotCallIDs) != 2 || got["c1"] == nil {
		t.Errorf("GetByCallIDs delegation failed: got=%v ids=%v", got, repo.gotCallIDs)
	}
}

func TestBillingQueryPropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	uc := NewBillingQueryUseCase(&billingRepoStub{err: sentinel})
	if _, err := uc.GetByCallID("c1"); !errors.Is(err, sentinel) {
		t.Fatalf("expected error to propagate, got %v", err)
	}
}
