package workspace_pricing

import (
	"errors"
	"fmt"

	"vozko/domain/carriermessaging"
)

var ErrCarrierServiceUnsupported = errors.New("unsupported carrier messaging service for pricing")

func CarrierMetricFor(channel carriermessaging.Channel, service string) (string, error) {
	for _, kind := range carriermessaging.ChargeKindsOf(channel) {
		if kind.PricingService() == service {
			return kind.PricingMetric(), nil
		}
	}
	return "", ErrCarrierServiceUnsupported
}

func (p *pricer) PriceRCS(workspaceID string, service string) (PriceResult, error) {
	return p.priceCarrier(workspaceID, carriermessaging.ChannelRCS, CategoryRCS, service)
}

func (p *pricer) PriceSMS(workspaceID string, service string) (PriceResult, error) {
	return p.priceCarrier(workspaceID, carriermessaging.ChannelSMS, CategorySMS, service)
}

func (p *pricer) priceCarrier(workspaceID string, channel carriermessaging.Channel, category ServiceCategory, service string) (PriceResult, error) {
	metric, err := CarrierMetricFor(channel, service)
	if err != nil {
		return PriceResult{}, err
	}
	resolved, err := p.ResolveForWorkspace(workspaceID)
	if err != nil {
		return PriceResult{}, err
	}
	price, ok := PriceOf(resolved, category, service, metric)
	if !ok {
		return PriceResult{}, fmt.Errorf("%w: %s/%s/%s (workspace %s)", ErrPricingItemNotFound, category, service, metric, workspaceID)
	}
	return price, nil
}
