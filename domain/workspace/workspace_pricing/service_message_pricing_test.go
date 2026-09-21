package workspace_pricing

import (
	"testing"

	"vozko/domain/conversation"
)

// From 1 October 2026 Meta charges for service messages, the free-form replies
// an agent or the AI sends inside the 24 hour window. They need a line in the
// catalog so a plan can carry a price for them, the same way the three template
// categories already do.
//
// Nothing charges this line yet. It exists so the price can be set once Meta
// publishes its rate card, and so the admin plan editor has somewhere to put
// it, and these tests pin that it is wired in without being wired up.

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

// The line has to exist, or the plan editor has no row to render and an admin
// has nowhere to record the rate when Meta publishes it.
func TestServiceMessagesAreInTheCatalog(t *testing.T) {
	item := catalogEntry(t, CategoryWhatsApp, WhatsAppServiceServiceMessage, "per_message")

	if item.Currency != "USD" {
		t.Errorf("currency = %q, want USD like every other WhatsApp line", item.Currency)
	}
}

// It is deliberately unpriced.
//
// Meta prices service messages per recipient market and the rate card is not
// ours to guess, so seeding a number would put a figure in front of an operator
// that we invented. Zero means "nobody has priced this yet", which is the
// truth, and the estimate rail already skips rows with no price rather than
// dividing by zero.
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

// A service message is NOT a template, and this is the seam where that could go
// wrong quietly. If the template normalizer ever resolved to the service line,
// a template send would be priced on a row that costs nothing and the customer
// would stop being charged for marketing.
func TestTemplateNormalizerNeverResolvesToServiceMessages(t *testing.T) {
	for _, raw := range []string{"SERVICE", "service", "Service"} {
		got, err := NormalizeWhatsAppTemplateService(raw)
		if err == nil {
			t.Errorf("NormalizeWhatsAppTemplateService(%q) = %q, want it rejected: a service message is not a template", raw, got)
		}
	}

	// The three that ARE templates still resolve, so this test cannot pass by
	// having broken the normalizer outright.
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

// The catalog key must be Meta's own category name, because that is what the
// status webhook stamps on the message and what the cost report counts by. Two
// spellings of the same concept would mean the line an admin prices and the
// messages it is supposed to price could never be joined.
func TestServiceKeyMatchesMetaPricingCategory(t *testing.T) {
	if WhatsAppServiceServiceMessage != conversation.MetaPricingCategoryService {
		t.Errorf("catalog service = %q but Meta stamps %q on the message",
			WhatsAppServiceServiceMessage, conversation.MetaPricingCategoryService)
	}
}

// Adding a line must not disturb the three that are charged today.
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

// The catalog is seeded by key, so a duplicate would collide on the unique
// index (workspace_id, category, service, metric) at boot.
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

// The export feeds the plan editor's defaults, so a line missing from it is a
// line no plan can ever price.
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

// The line must resolve to no charge, which is what "added but not charging
// yet" actually means at the layer that decides money.
//
// PriceWhatsAppTemplate refuses an item whose PriceMicros is not positive, so
// an unpriced row cannot produce a debit even if some future caller looked it
// up by name. This pins that guard together with the zero, because either one
// alone would let a charge through if the other changed.
func TestServiceMessagesResolveToNoCharge(t *testing.T) {
	resolved := ResolvePricingLayers(DefaultPricingCatalog)

	item := findResolvedItem(resolved, CategoryWhatsApp, WhatsAppServiceServiceMessage, "per_message")
	if item == nil {
		t.Fatal("the service message line does not resolve at all")
	}
	if item.PriceMicros > 0 {
		t.Errorf("priceMicros = %d; a positive price here would start charging customers", item.PriceMicros)
	}

	// The three template lines still resolve to a real price, so this test
	// cannot pass by having emptied the catalog.
	for _, service := range []string{WhatsAppServiceUtility, WhatsAppServiceMarketing, WhatsAppServiceAuthentication} {
		priced := findResolvedItem(resolved, CategoryWhatsApp, service, "per_message")
		if priced == nil || priced.PriceMicros <= 0 {
			t.Errorf("%s resolved to no price; template sends would stop being charged", service)
		}
	}
}
