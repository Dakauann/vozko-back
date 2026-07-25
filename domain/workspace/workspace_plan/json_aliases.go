package workspace_plan

import (
	"encoding/json"

	"vozko/brand"
	"vozko/domain/branding"
)

func (p PlanPricingItem) MarshalJSON() ([]byte, error) {
	type rawPlanPricingItem PlanPricingItem

	rawBytes, err := json.Marshal(rawPlanPricingItem(p))
	if err != nil {
		return nil, err
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rawBytes, &payload); err != nil {
		return nil, err
	}

	payload["service"] = branding.ExternalModelID(p.Service, brand.AliasPrefix())
	return json.Marshal(payload)
}

func (i *PlanPricingItemInput) UnmarshalJSON(data []byte) error {
	type rawPlanPricingItemInput PlanPricingItemInput
	aux := rawPlanPricingItemInput{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*i = PlanPricingItemInput(aux)
	i.Service = branding.InternalModelID(i.Service, brand.AliasPrefix())
	return nil
}
