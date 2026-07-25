package balance

import (
	balancedomain "vozko/domain/balance"
)

func (r *CreateBalanceRequest) Validate() map[string]string {
	if r.WorkspaceID == "" {
		return map[string]string{"workspaceId": "required"}
	}
	return nil
}

func (r *CreditDebitRequest) Validate() map[string]string {
	if r.Amount <= 0 {
		return map[string]string{"amount": "must be positive"}
	}
	if !balancedomain.ServiceType(r.ServiceType).IsValid() {
		return map[string]string{"serviceType": "must be voice_campaign, whatsapp_campaign, manual_adjustment, or top_up"}
	}
	return nil
}

func (r *CreditResourceRequest) Validate() map[string]string {
	if r.Amount <= 0 {
		return map[string]string{"amount": "must be positive"}
	}
	if !balancedomain.ServiceType(r.ServiceType).IsValid() {
		return map[string]string{"serviceType": "must be voice_campaign, whatsapp_campaign, manual_adjustment, or top_up"}
	}
	return nil
}
