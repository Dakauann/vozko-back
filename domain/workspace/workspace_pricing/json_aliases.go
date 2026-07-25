package workspace_pricing

import (
	"encoding/json"

	"vozko/brand"
	"vozko/domain/branding"
)

func marshalPricingServiceAlias(rawPayload interface{}, category string, service string) ([]byte, error) {
	rawBytes, err := json.Marshal(rawPayload)
	if err != nil {
		return nil, err
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rawBytes, &payload); err != nil {
		return nil, err
	}

	payload["service"] = branding.ExternalModelID(service, brand.AliasPrefix())
	return json.Marshal(payload)
}

func (p PricingItem) MarshalJSON() ([]byte, error) {
	type rawPricingItem PricingItem
	return marshalPricingServiceAlias(rawPricingItem(p), string(p.Category), p.Service)
}

func (p ResolvedPricingItem) MarshalJSON() ([]byte, error) {
	type rawResolvedPricingItem ResolvedPricingItem
	return marshalPricingServiceAlias(rawResolvedPricingItem(p), string(p.Category), p.Service)
}

func (e PricingAuditEntry) MarshalJSON() ([]byte, error) {
	type rawPricingAuditEntry PricingAuditEntry
	return marshalPricingServiceAlias(rawPricingAuditEntry(e), string(e.Category), e.Service)
}

func (i *UpdatePricingItemInput) UnmarshalJSON(data []byte) error {
	type rawInput UpdatePricingItemInput
	aux := rawInput{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*i = UpdatePricingItemInput(aux)
	i.Service = branding.InternalModelID(i.Service, brand.AliasPrefix())
	return nil
}
