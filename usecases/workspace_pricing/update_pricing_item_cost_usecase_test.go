package workspace_pricing_usecase

import (
	"errors"
	"testing"

	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type pricingRepo struct {
	workspace_pricing.Repository
	items  []workspace_pricing.PricingItem
	saved  *workspace_pricing.PricingItem
	audits []workspace_pricing.PricingAuditEntry
}

func (r *pricingRepo) ListDefaultPricingItems() ([]workspace_pricing.PricingItem, error) {
	return r.items, nil
}

func (r *pricingRepo) UpsertPricingItem(item *workspace_pricing.PricingItem) error {
	copied := *item
	r.saved = &copied
	return nil
}

func (r *pricingRepo) GetPricingItem(string) (*workspace_pricing.PricingItem, error) {
	return r.saved, nil
}

func (r *pricingRepo) CreateAuditEntry(entry *workspace_pricing.PricingAuditEntry) error {
	r.audits = append(r.audits, *entry)
	return nil
}

func serviceRow() workspace_pricing.PricingItem {
	return workspace_pricing.PricingItem{ID: "svc", Category: workspace_pricing.CategoryWhatsApp, Service: workspace_pricing.WhatsAppServiceServiceMessage, Metric: "per_message", Currency: "USD"}
}

func costInput(costMicros int64) workspace_pricing.UpdatePricingItemCostInput {
	return workspace_pricing.UpdatePricingItemCostInput{Category: workspace_pricing.CategoryWhatsApp, Service: workspace_pricing.WhatsAppServiceServiceMessage, Metric: "per_message", CostMicros: costMicros}
}

func TestSettingTheCostLeavesThePriceUntouchedAndIsAudited(t *testing.T) {
	repo := &pricingRepo{items: []workspace_pricing.PricingItem{serviceRow()}}
	item, err := NewUpdatePricingItemCostUseCase(repo).Execute(costInput(4_000), "admin-1")
	if err != nil {
		t.Fatal(err)
	}
	if item.CostMicros != 4_000 || item.PriceMicros != 0 {
		t.Fatalf("item %+v", item)
	}
	if len(repo.audits) != 1 || repo.audits[0].OldCostMicros != 0 || repo.audits[0].NewCostMicros != 4_000 || repo.audits[0].NewPriceMicros != 0 || repo.audits[0].ChangedBy != "admin-1" {
		t.Fatalf("audit %+v", repo.audits)
	}
}

func TestACostMustBePositive(t *testing.T) {
	repo := &pricingRepo{items: []workspace_pricing.PricingItem{serviceRow()}}
	if _, err := NewUpdatePricingItemCostUseCase(repo).Execute(costInput(0), "admin-1"); !errors.Is(err, workspace_pricing.ErrCostMicrosNotPositive) {
		t.Fatalf("err %v", err)
	}
	if repo.saved != nil {
		t.Fatal("nothing is saved")
	}
}

func TestACostIsSetOnlyOnAnExistingCatalogRow(t *testing.T) {
	repo := &pricingRepo{}
	if _, err := NewUpdatePricingItemCostUseCase(repo).Execute(costInput(4_000), "admin-1"); !errors.Is(err, workspace_pricing.ErrPricingItemNotFound) {
		t.Fatalf("err %v", err)
	}
}

func TestACostIsSetOnlyOnAConfigurableCategory(t *testing.T) {
	input := costInput(4_000)
	input.Category = workspace_pricing.CategoryExchangeRate
	if _, err := NewUpdatePricingItemCostUseCase(&pricingRepo{}).Execute(input, "admin-1"); !errors.Is(err, workspace_pricing.ErrCategoryNotConfigurable) {
		t.Fatalf("err %v", err)
	}
}
