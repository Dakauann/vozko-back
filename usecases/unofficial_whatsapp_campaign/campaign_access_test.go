package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type campaignGetStub map[string]*uwc.Campaign

func (g campaignGetStub) Execute(_ context.Context, id string) (*uwc.Campaign, error) {
	c, ok := g[id]
	if !ok {
		return nil, uwc.ErrCampaignNotFound
	}
	return c, nil
}

var accessCampaigns = campaignGetStub{
	"c-mine":  {ID: "c-mine", WorkspaceID: "ws1", DepartmentID: "d-sales"},
	"c-other": {ID: "c-other", WorkspaceID: "ws2", DepartmentID: "d-sales"},
	"c-team":  {ID: "c-team", WorkspaceID: "ws1", DepartmentID: "d-support"},
}

func TestCampaignAccessHidesAnotherWorkspacesCampaign(t *testing.T) {
	if _, err := NewCampaignAccessUseCase(accessCampaigns).Owned(context.Background(), "ws1", uw.Unrestricted(), "c-other"); !errors.Is(err, uwc.ErrCampaignNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestCampaignAccessRespectsTheDepartmentScope(t *testing.T) {
	sales := uw.DepartmentScope{DepartmentIDs: []string{"d-sales"}, Restrict: true}
	uc := NewCampaignAccessUseCase(accessCampaigns)
	if _, err := uc.Owned(context.Background(), "ws1", sales, "c-team"); !errors.Is(err, uwc.ErrCampaignNotFound) {
		t.Fatalf("other department: err = %v", err)
	}
	if c, err := uc.Owned(context.Background(), "ws1", sales, "c-mine"); err != nil || c.ID != "c-mine" {
		t.Fatalf("own campaign: %v %v", c, err)
	}
}

func TestCampaignAccessRequiresAWorkspace(t *testing.T) {
	if _, err := NewCampaignAccessUseCase(accessCampaigns).Owned(context.Background(), "", uw.Unrestricted(), "c-mine"); !errors.Is(err, uwc.ErrCampaignNotFound) {
		t.Fatalf("err = %v", err)
	}
}
