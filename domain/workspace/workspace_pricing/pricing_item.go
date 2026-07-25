package workspace_pricing

import (
	"errors"
	"strings"
	"time"
)

type ServiceCategory string

const (
	CategoryLLM          ServiceCategory = "llm"
	CategoryWhatsApp     ServiceCategory = "whatsapp"
	CategoryTelephony    ServiceCategory = "telephony"
	CategoryExchangeRate ServiceCategory = "exchange_rate"
	CategoryMargin       ServiceCategory = "margin"
)

const SuperAdminEmail = "dakauannc@gmail.com"

var ErrPricingItemNotFound = errors.New("pricing item not found")
var ErrWhatsAppTemplateCategoryUnsupported = errors.New("unsupported whatsapp template category for pricing")
var ErrPriceMicrosNotPositive = errors.New("priceMicros must be greater than zero")

const (
	WhatsAppServiceUtility        = "utility"
	WhatsAppServiceMarketing      = "marketing"
	WhatsAppServiceAuthentication = "authentication"
)

const (
	TelephonyServiceSIPTrunk      = "sip_trunk"
	TelephonyServiceWhatsAppCalls = "whatsapp_calls"
)

const TelephonyChannelWhatsApp = "whatsapp"

func TelephonyServiceForChannel(channel string) string {
	if strings.EqualFold(strings.TrimSpace(channel), TelephonyChannelWhatsApp) {
		return TelephonyServiceWhatsAppCalls
	}
	return TelephonyServiceSIPTrunk
}

type PricingItem struct {
	ID          string          `json:"id"`
	WorkspaceID *string         `json:"workspaceId"`
	Category    ServiceCategory `json:"category"`
	Service     string          `json:"service"`
	Metric      string          `json:"metric"`
	CostMicros  int64           `json:"costMicros"`
	PriceMicros int64           `json:"priceMicros"`
	MarkupPct   float64         `json:"markupPct"`
	Currency    string          `json:"currency"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

type ResolvedPricingItem struct {
	Category           ServiceCategory `json:"category"`
	Service            string          `json:"service"`
	Metric             string          `json:"metric"`
	CostMicros         int64           `json:"costMicros"`
	PriceMicros        int64           `json:"priceMicros"`
	MarkupPct          float64         `json:"markupPct"`
	Currency           string          `json:"currency"`
	IsOverride         bool            `json:"isOverride"`
	DefaultCostMicros  int64           `json:"defaultCostMicros"`
	DefaultPriceMicros int64           `json:"defaultPriceMicros"`
	DefaultMarkupPct   float64         `json:"defaultMarkupPct"`
	OverrideID         *string         `json:"overrideId,omitempty"`
	UpdatedAt          time.Time       `json:"updatedAt"`
}

var ErrCategoryNotConfigurable = errors.New("this pricing category is not admin-configurable")
var ErrSuperAdminRequired = errors.New("only the super admin can modify exchange rate")

var configurableCategories = map[ServiceCategory]bool{
	CategoryLLM:       true,
	CategoryWhatsApp:  true,
	CategoryTelephony: true,
}

func IsCategoryConfigurable(c ServiceCategory) bool {
	return configurableCategories[c]
}

var DefaultPricingCatalog = []PricingItem{

	{Category: CategoryWhatsApp, Service: WhatsAppServiceUtility, Metric: "per_message", CostMicros: 6_800, PriceMicros: 16_667, Currency: "USD"},
	{Category: CategoryWhatsApp, Service: WhatsAppServiceMarketing, Metric: "per_message", CostMicros: 62_500, PriceMicros: 66_667, Currency: "USD"},
	{Category: CategoryWhatsApp, Service: WhatsAppServiceAuthentication, Metric: "per_message", CostMicros: 6_800, PriceMicros: 16_667, Currency: "USD"},

	{Category: CategoryTelephony, Service: TelephonyServiceSIPTrunk, Metric: "per_minute", CostMicros: 4_167, PriceMicros: 8_333, Currency: "USD"},

	// WhatsApp calls are priced independently from the SIP trunk: cost $0.01080/min,
	// price $0.013333/min (≈ R$0.08/min at the 6.0 USD→BRL default rate).
	{Category: CategoryTelephony, Service: TelephonyServiceWhatsAppCalls, Metric: "per_minute", CostMicros: 10_800, PriceMicros: 13_333, Currency: "USD"},

	{Category: CategoryLLM, Service: "default_markup", Metric: "percentage", MarkupPct: 0.20, Currency: "USD"},

	{Category: CategoryExchangeRate, Service: "usd_to_brl", Metric: "per_unit", PriceMicros: 6_000_000, Currency: "BRL"},
}

func itemKey(category ServiceCategory, service, metric string) string {
	return string(category) + "|" + service + "|" + metric
}

type CatalogEntryExport struct {
	Category    string
	Service     string
	Metric      string
	CostMicros  int64
	PriceMicros int64
	MarkupPct   float64
	Currency    string
}

func ExportDefaultCatalog() []CatalogEntryExport {
	out := make([]CatalogEntryExport, len(DefaultPricingCatalog))
	for i, item := range DefaultPricingCatalog {
		out[i] = CatalogEntryExport{
			Category:    string(item.Category),
			Service:     item.Service,
			Metric:      item.Metric,
			CostMicros:  item.CostMicros,
			PriceMicros: item.PriceMicros,
			MarkupPct:   item.MarkupPct,
			Currency:    item.Currency,
		}
	}
	return out
}

func NormalizeWhatsAppTemplateService(templateCategory string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(templateCategory)) {
	case "UTILITY":
		return WhatsAppServiceUtility, nil
	case "MARKETING":
		return WhatsAppServiceMarketing, nil
	case "AUTHENTICATION":
		return WhatsAppServiceAuthentication, nil
	default:
		return "", ErrWhatsAppTemplateCategoryUnsupported
	}
}

func ResolvePricingLayers(defaults []PricingItem, overlays ...[]PricingItem) []ResolvedPricingItem {
	overrideMap := make(map[string]*PricingItem)
	for _, items := range overlays {
		for i := range items {

			if items[i].Category == CategoryExchangeRate {
				continue
			}
			k := itemKey(items[i].Category, items[i].Service, items[i].Metric)
			overrideMap[k] = &items[i]
		}
	}

	resolved := make([]ResolvedPricingItem, 0, len(defaults))
	for _, d := range defaults {
		k := itemKey(d.Category, d.Service, d.Metric)
		r := ResolvedPricingItem{
			Category:           d.Category,
			Service:            d.Service,
			Metric:             d.Metric,
			CostMicros:         d.CostMicros,
			PriceMicros:        d.PriceMicros,
			MarkupPct:          d.MarkupPct,
			Currency:           d.Currency,
			DefaultCostMicros:  d.CostMicros,
			DefaultPriceMicros: d.PriceMicros,
			DefaultMarkupPct:   d.MarkupPct,
			UpdatedAt:          d.UpdatedAt,
		}
		if o, ok := overrideMap[k]; ok {
			if o.CostMicros > 0 {
				r.CostMicros = o.CostMicros
			}
			if o.PriceMicros > 0 {
				r.PriceMicros = o.PriceMicros
			}
			if o.MarkupPct > 0 {
				r.MarkupPct = o.MarkupPct
			}
			r.IsOverride = true
			r.OverrideID = &o.ID
			r.UpdatedAt = o.UpdatedAt
		}
		resolved = append(resolved, r)
	}
	return resolved
}
