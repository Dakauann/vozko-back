package workspace_pricing_repository

import (
	"testing"

	"github.com/google/uuid"

	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func defaultRow(t *testing.T, repo workspace_pricing.Repository, service, metric string) workspace_pricing.PricingItem {
	t.Helper()
	items, err := repo.ListDefaultPricingItems()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Category == workspace_pricing.CategoryEmail && item.Service == service && item.Metric == metric {
			return item
		}
	}
	t.Fatalf("no default email %s/%s row", service, metric)
	return workspace_pricing.PricingItem{}
}

func TestSeedDefaultsCorrectsTheStaleReceiveCostAndKeepsThePrice(t *testing.T) {
	db := repotest.IsolatedDB(t, "pricing_seed", &schema.PricingItem{})
	stale := schema.PricingItem{
		ID: uuid.NewString(), Category: string(workspace_pricing.CategoryEmail), Service: workspace_pricing.EmailServiceReceive,
		Metric: workspace_pricing.EmailMetricPerMessage, CostMicros: 100, PriceMicros: 640, Currency: "USD",
	}
	if err := db.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	for range 2 {
		if err := repo.SeedDefaults(workspace_pricing.DefaultPricingCatalog); err != nil {
			t.Fatal(err)
		}
	}
	receive := defaultRow(t, repo, workspace_pricing.EmailServiceReceive, workspace_pricing.EmailMetricPerMessage)
	if receive.ID != stale.ID || receive.CostMicros != 190 || receive.PriceMicros != 640 {
		t.Fatalf("receive row = %+v, want the same row at cost 190 with its price kept", receive)
	}
	attachments := defaultRow(t, repo, workspace_pricing.EmailServiceSend, workspace_pricing.EmailMetricPerChunk)
	if attachments.CostMicros != 29 || attachments.PriceMicros != 102 {
		t.Fatalf("send per chunk row = %+v", attachments)
	}
}

func TestSeedDefaultsKeepsACostAnAdminEdited(t *testing.T) {
	db := repotest.IsolatedDB(t, "pricing_seed", &schema.PricingItem{})
	edited := schema.PricingItem{
		ID: uuid.NewString(), Category: string(workspace_pricing.CategoryEmail), Service: workspace_pricing.EmailServiceReceive,
		Metric: workspace_pricing.EmailMetricPerMessage, CostMicros: 210, PriceMicros: 583, Currency: "USD",
	}
	if err := db.Create(&edited).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	if err := repo.SeedDefaults(workspace_pricing.DefaultPricingCatalog); err != nil {
		t.Fatal(err)
	}
	if got := defaultRow(t, repo, workspace_pricing.EmailServiceReceive, workspace_pricing.EmailMetricPerMessage); got.CostMicros != 210 {
		t.Fatalf("receive cost = %d, an edited cost must stay", got.CostMicros)
	}
}
