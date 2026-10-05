package pricing_analytics

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	analytics_domain "vozko/domain/analytics"
	"vozko/infra/meta"
)

type Gateway struct {
	client *meta.Client
}

func NewGateway(client *meta.Client) *Gateway {
	return &Gateway{client: client}
}

type pricingAnalyticsResponse struct {
	PricingAnalytics struct {
		Data []struct {
			DataPoints []struct {
				PricingCategory string `json:"pricing_category"`
				PricingType     string `json:"pricing_type"`
				Volume          int64  `json:"volume"`
			} `json:"data_points"`
		} `json:"data"`
	} `json:"pricing_analytics"`
}

func (g *Gateway) Volumes(ctx context.Context, wabaID, accessToken string, start, end time.Time) ([]analytics_domain.MetaVolumePoint, error) {
	fields := fmt.Sprintf(`pricing_analytics.start(%d).end(%d).granularity(DAILY).metric_types(["VOLUME"]).dimensions(["PRICING_CATEGORY","PRICING_TYPE"])`, start.Unix(), end.Unix())
	var out pricingAnalyticsResponse
	err := g.client.Do(ctx, meta.Request{
		Method: http.MethodGet,
		Path:   "/" + url.PathEscape(wabaID),
		Token:  accessToken,
		Query:  url.Values{"fields": {fields}},
	}, &out)
	if err != nil {
		return nil, err
	}
	var points []analytics_domain.MetaVolumePoint
	for _, series := range out.PricingAnalytics.Data {
		for _, p := range series.DataPoints {
			points = append(points, analytics_domain.MetaVolumePoint{Category: p.PricingCategory, PricingType: p.PricingType, Volume: p.Volume})
		}
	}
	return points, nil
}
