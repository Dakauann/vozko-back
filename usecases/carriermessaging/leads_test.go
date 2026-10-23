package carriermessaging_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/lead"
)

type fakeLeadStore struct {
	existing *lead.Lead
	created  []lead.LeadUpdate
	err      error
}

func (f *fakeLeadStore) FindByNumber(_, _ string) (*lead.Lead, error) {
	if f.existing == nil {
		return nil, lead.ErrLeadNotFound
	}
	return f.existing, nil
}

func (f *fakeLeadStore) FindOrCreate(ws, number string, update lead.LeadUpdate) (*lead.Lead, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	f.created = append(f.created, update)
	return &lead.Lead{ID: "new", WorkspaceID: ws, Number: number, Name: update.Name}, true, nil
}

func TestLeadResolverKeepsTheExistingLead(t *testing.T) {
	store := &fakeLeadStore{existing: &lead.Lead{ID: "l1", Number: "5511999990000", Blocked: true}}
	got, err := NewLeadResolver(store).EnsureLead(context.Background(), "ws-1", "5511999990000", "Ana")
	if err != nil || got.ID != "l1" || !got.Blocked || len(store.created) != 0 {
		t.Fatalf("EnsureLead = %+v, %v", got, err)
	}
}

func TestLeadResolverCreatesAChannelLead(t *testing.T) {
	store := &fakeLeadStore{}
	got, err := NewLeadResolver(store).EnsureLead(context.Background(), "ws-1", "5511999990000", "Ana")
	if err != nil || got.ID != "new" || store.created[0].Source != lead.SourceChannel || store.created[0].Name != "Ana" {
		t.Fatalf("EnsureLead = %+v, %v, created = %+v", got, err, store.created)
	}
}

func TestLeadResolverFailsClosed(t *testing.T) {
	if _, err := NewLeadResolver(&fakeLeadStore{}).EnsureLead(context.Background(), "ws-1", "abc", ""); err == nil {
		t.Fatal("a bad number must not resolve")
	}
	store := &fakeLeadStore{err: errors.New("db down")}
	if _, err := NewLeadResolver(store).EnsureLead(context.Background(), "ws-1", "5511999990000", ""); err == nil {
		t.Fatal("a store failure must be returned")
	}
}
