package whatsapp_campaign_usecase

import (
	"context"
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
	wc "vozko/domain/whatsapp_campaign"
)

type denyGrants struct{ asked string }

func (d *denyGrants) HasAccess(workspaceID, templateID string) (bool, error) {
	d.asked = workspaceID + "|" + templateID
	return false, nil
}

func TestCreateCampaignRefusesATemplateNotGrantedToTheWorkspace(t *testing.T) {
	businessPhones := newOwnershipBusinessPhoneRepo()
	businessPhones.phones["phone-1"] = &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-1", OwnerWorkspaceID: "ws-1"}
	grants := &denyGrants{}
	uc := NewCreateCampaignUseCase(nil, nil, nil, nil, businessPhones, &ownershipWorkspacePhoneAccessRepo{hasAccess: true}, nil, nil)
	uc.(*createCampaignUseCase).SetTemplateGrants(grants)

	_, err := uc.Execute(context.Background(), &wc.Campaign{
		WorkspaceID: "ws-1", Name: "Campaign", TemplateID: "tmpl-other", BusinessPhoneID: "phone-1",
		Status: wc.CampaignStatusStopped, PhoneInputs: []wc.PhoneInput{{Number: "5511999999999", Variables: []string{"Alice"}}},
	})
	if err != wc.ErrCampaignTemplateNotFound || grants.asked != "ws-1|tmpl-other" {
		t.Fatalf("err %v asked %q", err, grants.asked)
	}
}
