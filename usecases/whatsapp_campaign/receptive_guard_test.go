package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"

	wc "vozko/domain/whatsapp_campaign"
)

type guardRepoStub struct {
	wc.Repository
	stored  *wc.Campaign
	deleted bool
}

func (s *guardRepoStub) FindByID(string) (*wc.Campaign, error) {
	copied := *s.stored
	return &copied, nil
}

func (s *guardRepoStub) Delete(string) error {
	s.deleted = true
	return nil
}

func TestReceptiveCannotBeCreatedAsACampaign(t *testing.T) {
	uc := NewCreateCampaignUseCase(&guardRepoStub{}, nil, nil, nil, nil, nil, nil, nil)
	_, err := uc.Execute(context.Background(), &wc.Campaign{Name: "Receptivo", Type: wc.CampaignTypeOrganic, BusinessPhoneID: "phone-1", WorkspaceID: "ws-1"})
	if !errors.Is(err, wc.ErrReceptiveManagedByNumber) {
		t.Fatalf("err %v", err)
	}
}

func TestReceptiveCannotBeEditedAsACampaign(t *testing.T) {
	cases := []struct {
		name   string
		stored wc.CampaignType
		input  wc.CampaignType
	}{
		{"editing a receptive container", wc.CampaignTypeOrganic, ""},
		{"turning an outbound campaign into receptive", wc.CampaignTypeStandard, wc.CampaignTypeOrganic},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &guardRepoStub{stored: &wc.Campaign{ID: "c1", Type: tc.stored, Name: "x", BusinessPhoneID: "phone-1"}}
			_, err := NewUpdateCampaignUseCase(repo, nil, nil, nil, nil).Execute("c1", &wc.Campaign{Name: "y", Type: tc.input, EnableAgentResponses: true})
			if !errors.Is(err, wc.ErrReceptiveManagedByNumber) {
				t.Fatalf("err %v", err)
			}
		})
	}
}

func TestReceptiveConversationsCannotBeDeletedAsACampaign(t *testing.T) {
	repo := &guardRepoStub{stored: &wc.Campaign{ID: "c1", Type: wc.CampaignTypeOrganic}}
	err := NewDeleteCampaignUseCase(repo, nil).Execute("c1")
	if !errors.Is(err, wc.ErrReceptiveManagedByNumber) || repo.deleted {
		t.Fatalf("err %v deleted %v", err, repo.deleted)
	}
}
