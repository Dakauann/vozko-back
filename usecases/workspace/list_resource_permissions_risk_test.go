package workspace_usecase

import (
	"testing"

	"vozko/domain/workspace"
)

func TestResourcePermissionCatalogCarriesTheRisks(t *testing.T) {
	var campaigns *workspace.ResourcePermissionInfo
	for _, info := range NewListResourcePermissionsUseCase().Execute() {
		if info.Resource == workspace.ResourceWhatsAppCampaigns {
			campaigns = &info
		}
	}
	if campaigns == nil {
		t.Fatal("campaigns missing from the catalog")
	}
	if len(campaigns.Risks["start"]) != 2 || campaigns.Risks["start"][0].Level != workspace.RiskHigh {
		t.Fatalf("start risks = %+v", campaigns.Risks["start"])
	}
	if _, flagged := campaigns.Risks["read"]; flagged {
		t.Fatal("reading campaigns was flagged")
	}
}
