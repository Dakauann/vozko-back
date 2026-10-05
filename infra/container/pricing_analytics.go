package container

import (
	"log"

	"vozko/infra/meta"
	"vozko/infra/whatsapp/pricing_analytics"
)

func (c *Container) newPricingAnalyticsGateway() *pricing_analytics.Gateway {
	client, err := meta.NewClient(meta.Config{
		Host:       "graph.facebook.com",
		APIVersion: meta.DefaultGraphVersion,
		AppSecret:  c.cfg.MetaAppSecret,
	})
	if err != nil {
		log.Fatalf("Failed to build the Meta pricing analytics client: %v", err)
	}
	return pricing_analytics.NewGateway(client)
}
