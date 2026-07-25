package businessphone_usecase

import (
	businessphone "vozko/domain/whatsapp/business_phone"
	workspace_addon "vozko/domain/workspace/workspace_addon"
)

type phoneProvisioningGate struct {
	entitlements workspace_addon.GetWorkspaceEntitlementsUseCase
	phones       businessphone.OwnerPhoneReader
}

func NewPhoneProvisioningGate(
	entitlements workspace_addon.GetWorkspaceEntitlementsUseCase,
	phones businessphone.OwnerPhoneReader,
) businessphone.ProvisioningGate {
	return &phoneProvisioningGate{entitlements: entitlements, phones: phones}
}

func (g *phoneProvisioningGate) CanProvisionPhone(workspaceID string) (bool, error) {
	limit, err := g.whatsAppEntitlement(workspaceID)
	if err != nil {
		return false, err
	}
	count, err := g.phones.CountActiveDialog360ByOwner(workspaceID)
	if err != nil {
		return false, err
	}
	return count < int64(limit), nil
}

func (g *phoneProvisioningGate) whatsAppEntitlement(workspaceID string) (int, error) {
	ents, err := g.entitlements.Execute(workspaceID)
	if err != nil {
		return 0, err
	}
	for _, e := range ents {
		if e.Kind == workspace_addon.EntitlementWhatsAppBusinessPhones {
			return e.Total, nil
		}
	}
	return 0, nil
}
