package workspace_pricing

import (
	"testing"

	"vozko/domain/conversation"
)

func catalogEntry(t *testing.T, category ServiceCategory, service, metric string) PricingItem {
	t.Helper()
	for _, item := range DefaultPricingCatalog {
		if item.Category == category && item.Service == service && item.Metric == metric {
			return item
		}
	}
	t.Fatalf("no catalog entry for %s|%s|%s", category, service, metric)
	return PricingItem{}
}

func TestServiceMessagesAreInTheCatalog(t *testing.T) {
	item := catalogEntry(t, CategoryWhatsApp, WhatsAppServiceServiceMessage, "per_message")

	if item.Currency != "USD" {
		t.Errorf("currency = %q, want USD like every other WhatsApp line", item.Currency)
	}
}

func TestServiceMessagesAreSeededUnpriced(t *testing.T) {
	item := catalogEntry(t, CategoryWhatsApp, WhatsAppServiceServiceMessage, "per_message")

	if item.PriceMicros != 0 {
		t.Errorf("priceMicros = %d, want 0 until Meta publishes a rate we can attach", item.PriceMicros)
	}
	if item.CostMicros != 0 {
		t.Errorf("costMicros = %d, want 0 until Meta publishes a rate we can attach", item.CostMicros)
	}
	if item.MarkupPct != 0 {
		t.Errorf("markupPct = %v, want 0; this line is not charged yet", item.MarkupPct)
	}
}

func TestTemplateNormalizerNeverResolvesToServiceMessages(t *testing.T) {
	for _, raw := range []string{"SERVICE", "service", "Service"} {
		got, err := NormalizeWhatsAppTemplateService(raw)
		if err == nil {
			t.Errorf("NormalizeWhatsAppTemplateService(%q) = %q, want it rejected: a service message is not a template", raw, got)
		}
	}

	for raw, want := range map[string]string{
		"UTILITY":        WhatsAppServiceUtility,
		"MARKETING":      WhatsAppServiceMarketing,
		"AUTHENTICATION": WhatsAppServiceAuthentication,
	} {
		got, err := NormalizeWhatsAppTemplateService(raw)
		if err != nil || got != want {
			t.Errorf("NormalizeWhatsAppTemplateService(%q) = (%q, %v), want %q", raw, got, err, want)
		}
	}
}

func TestServiceKeyMatchesMetaPricingCategory(t *testing.T) {
	if WhatsAppServiceServiceMessage != conversation.MetaPricingCategoryService {
		t.Errorf("catalog service = %q but Meta stamps %q on the message",
			WhatsAppServiceServiceMessage, conversation.MetaPricingCategoryService)
	}
}

func TestTemplateLinesAreUnchanged(t *testing.T) {
	for _, tc := range []struct {
		service     string
		costMicros  int64
		priceMicros int64
	}{
		{WhatsAppServiceUtility, 6_800, 16_667},
		{WhatsAppServiceMarketing, 62_500, 66_667},
		{WhatsAppServiceAuthentication, 6_800, 16_667},
	} {
		item := catalogEntry(t, CategoryWhatsApp, tc.service, "per_message")
		if item.CostMicros != tc.costMicros || item.PriceMicros != tc.priceMicros {
			t.Errorf("%s = cost %d price %d, want cost %d price %d",
				tc.service, item.CostMicros, item.PriceMicros, tc.costMicros, tc.priceMicros)
		}
	}
}

func TestCatalogKeysAreUnique(t *testing.T) {
	seen := make(map[string]bool, len(DefaultPricingCatalog))
	for _, item := range DefaultPricingCatalog {
		key := itemKey(item.Category, item.Service, item.Metric)
		if seen[key] {
			t.Errorf("duplicate catalog key %q; seeding would violate the unique index", key)
		}
		seen[key] = true
	}
}

func TestExportedCatalogCarriesTheServiceLine(t *testing.T) {
	for _, entry := range ExportDefaultCatalog() {
		if entry.Category == string(CategoryWhatsApp) &&
			entry.Service == WhatsAppServiceServiceMessage &&
			entry.Metric == "per_message" {
			return
		}
	}
	t.Error("the service message line is not exported, so the plan editor cannot show it")
}

func TestServiceMessagesResolveToNoCharge(t *testing.T) {
	resolved := ResolvePricingLayers(DefaultPricingCatalog)

	item := findResolvedItem(resolved, CategoryWhatsApp, WhatsAppServiceServiceMessage, "per_message")
	if item == nil {
		t.Fatal("the service message line does not resolve at all")
	}
	if item.PriceMicros > 0 {
		t.Errorf("priceMicros = %d; a positive price here would start charging customers", item.PriceMicros)
	}

	for _, service := range []string{WhatsAppServiceUtility, WhatsAppServiceMarketing, WhatsAppServiceAuthentication} {
		priced := findResolvedItem(resolved, CategoryWhatsApp, service, "per_message")
		if priced == nil || priced.PriceMicros <= 0 {
			t.Errorf("%s resolved to no price; template sends would stop being charged", service)
		}
	}
}
