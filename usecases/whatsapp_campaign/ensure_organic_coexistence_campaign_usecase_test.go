package whatsapp_campaign_usecase

import (
	"errors"
	"testing"

	wc "vozko/domain/whatsapp_campaign"
)

type ensureOrganicRepoStub struct {
	wc.Repository
	existing  *wc.Campaign
	findErr   error
	created   *wc.Campaign
	createErr error
}

func (s *ensureOrganicRepoStub) FindLatestOrganicByBusinessPhone(string, string) (*wc.Campaign, error) {
	return s.existing, s.findErr
}

func (s *ensureOrganicRepoStub) Create(c *wc.Campaign) error {
	s.created = c
	return s.createErr
}

func TestEnsureOrganicReturnsExistingWithoutCreating(t *testing.T) {
	repo := &ensureOrganicRepoStub{existing: &wc.Campaign{ID: "existing"}}
	uc := NewEnsureOrganicCoexistenceCampaignUseCase(repo)

	c, created, err := uc.Execute("ws1", "phone1", "+55 11 9999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created || c.ID != "existing" || repo.created != nil {
		t.Errorf("expected existing campaign and no create; got created=%v c=%v createCalled=%v", created, c, repo.created != nil)
	}
}

func TestEnsureOrganicCreatesWhenMissing(t *testing.T) {
	// Both a nil result and a lookup error must trigger creation.
	for _, tc := range []struct {
		name string
		repo *ensureOrganicRepoStub
	}{
		{"nil existing", &ensureOrganicRepoStub{existing: nil}},
		{"lookup error", &ensureOrganicRepoStub{findErr: errors.New("not found")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uc := NewEnsureOrganicCoexistenceCampaignUseCase(tc.repo)
			c, created, err := uc.Execute("ws1", "phone1", "+55 11 9999")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !created {
				t.Fatal("expected created=true")
			}
			if tc.repo.created == nil {
				t.Fatal("expected Create to be called")
			}
			if c.Type != wc.CampaignTypeOrganic || c.Status != wc.CampaignStatusRunning ||
				c.WorkspaceID != "ws1" || c.BusinessPhoneID != "phone1" || c.ID == "" {
				t.Errorf("created campaign has wrong fields: %+v", c)
			}
		})
	}
}

func TestEnsureOrganicPropagatesCreateError(t *testing.T) {
	sentinel := errors.New("boom")
	uc := NewEnsureOrganicCoexistenceCampaignUseCase(&ensureOrganicRepoStub{createErr: sentinel})
	if _, _, err := uc.Execute("ws1", "phone1", "x"); !errors.Is(err, sentinel) {
		t.Fatalf("expected create error to propagate, got %v", err)
	}
}
