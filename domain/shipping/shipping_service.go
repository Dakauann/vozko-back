package shipping

import (
	"context"
)

type FreightCalculator interface {
	CalculateFreight(ctx context.Context, origin, destination *Address, packages []Package) (*FreightQuote, error)
	GetAvailableServices(ctx context.Context, origin, destination *Address) ([]ShippingService, error)
}

type ShippingLabelGenerator interface {
}

type TrackingService interface {
	TrackPackage(ctx context.Context, trackingCode string) (*TrackingInfo, error)
	SubscribeTrackingWebhook(ctx context.Context, callbackURL string) error
}
