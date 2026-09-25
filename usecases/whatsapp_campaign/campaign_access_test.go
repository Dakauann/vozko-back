package whatsapp_campaign_usecase

import (
	"errors"
	"testing"

	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
)

type officialCampaigns map[string]*wc.Campaign

func (c officialCampaigns) Execute(id string) (*wc.Campaign, error) {
	found, ok := c[id]
	if !ok {
		return nil, wc.ErrCampaignNotFound
	}
	return found, nil
}

var accessible = officialCampaigns{
	"c-mine":  {ID: "c-mine", WorkspaceID: "ws1", DepartmentID: "d-sales"},
	"c-other": {ID: "c-other", WorkspaceID: "ws2", DepartmentID: "d-sales"},
	"c-team":  {ID: "c-team", WorkspaceID: "ws1", DepartmentID: "d-support"},
}

func TestOfficialCampaignAccessChecksWorkspaceAndDepartment(t *testing.T) {
	uc := NewCampaignAccessUseCase(accessible)
	owner := &wd.DepartmentFilter{IsOwnerOrAdmin: true}
	sales := &wd.DepartmentFilter{DepartmentIDs: []string{"d-sales"}, WorkspaceHasDepartments: true}
	if _, err := uc.Owned("ws1", owner, "c-other"); !errors.Is(err, wc.ErrCampaignNotFound) {
		t.Fatalf("foreign workspace: %v", err)
	}
	if _, err := uc.Owned("ws1", sales, "c-team"); !errors.Is(err, wc.ErrCampaignNotFound) {
		t.Fatalf("other department: %v", err)
	}
	if _, err := uc.Owned("ws1", nil, "c-mine"); !errors.Is(err, wc.ErrCampaignNotFound) {
		t.Fatalf("no filter: %v", err)
	}
	if c, err := uc.Owned("ws1", sales, "c-mine"); err != nil || c.ID != "c-mine" {
		t.Fatalf("own campaign: %v %v", c, err)
	}
}
