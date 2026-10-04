package whatsapp_campaign_usecase

import (
	"errors"
	"testing"

	wc "vozko/domain/whatsapp_campaign"
)

type ensureReceptiveRepoStub struct {
	wc.Repository
	existing  *wc.Campaign
	findErr   error
	created   *wc.Campaign
	createErr error
}

func (s *ensureReceptiveRepoStub) FindLatestOrganicByBusinessPhone(string, string) (*wc.Campaign, error) {
	return s.existing, s.findErr
}

func (s *ensureReceptiveRepoStub) Create(c *wc.Campaign) error {
	s.created = c
	return s.createErr
}

func TestEnsureReceptiveReturnsTheExistingContainer(t *testing.T) {
	repo := &ensureReceptiveRepoStub{existing: &wc.Campaign{ID: "existing"}}
	c, created, err := NewEnsureReceptiveContainerUseCase(repo).Execute("ws1", "phone1", "+55 11 9999")
	if err != nil || created || c.ID != "existing" || repo.created != nil {
		t.Fatalf("created=%v c=%v err=%v", created, c, err)
	}
}

func TestEnsureReceptiveCreatesTheContainerWhenTheNumberHasNone(t *testing.T) {
	repo := &ensureReceptiveRepoStub{findErr: wc.ErrCampaignNotFound}
	c, created, err := NewEnsureReceptiveContainerUseCase(repo).Execute("ws1", "phone1", "+55 11 9999")
	if err != nil || !created || repo.created == nil {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if !c.IsOrganic() || c.WorkspaceID != "ws1" || c.BusinessPhoneID != "phone1" || c.Name != "Receptivo +55 11 9999" {
		t.Fatalf("container %+v", c)
	}
}

func TestEnsureReceptiveNeverCreatesADuplicateWhenTheLookupFails(t *testing.T) {
	sentinel := errors.New("database down")
	repo := &ensureReceptiveRepoStub{findErr: sentinel}
	if _, _, err := NewEnsureReceptiveContainerUseCase(repo).Execute("ws1", "phone1", "x"); !errors.Is(err, sentinel) {
		t.Fatalf("err %v", err)
	}
	if repo.created != nil {
		t.Fatal("a failed lookup must not create a second container")
	}
}

func TestEnsureReceptivePropagatesCreateError(t *testing.T) {
	sentinel := errors.New("boom")
	repo := &ensureReceptiveRepoStub{findErr: wc.ErrCampaignNotFound, createErr: sentinel}
	if _, _, err := NewEnsureReceptiveContainerUseCase(repo).Execute("ws1", "phone1", "x"); !errors.Is(err, sentinel) {
		t.Fatalf("err %v", err)
	}
}
