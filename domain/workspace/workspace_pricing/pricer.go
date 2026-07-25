package workspace_pricing

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

type LLMPriceFetcher interface {
	FetchLLMPriceMicros(model string) (inputMicros, outputMicros int64, err error)
}

type VoiceCreditRateFetcher interface {
	FetchVoiceCreditRate(voiceID string) float64
}

type ModelCharCostFetcher interface {
	FetchModelCharCostMultiplier(modelID string) float64

	FetchModelPricingTier(modelID string) string
}

type PlanPricingProvider interface {
	ListForWorkspace(workspaceID string) ([]PricingItem, error)
}

type PriceResult struct {
	CostMicros   int64
	PriceMicros  int64
	ProfitMicros int64
}

type Pricer interface {
	ResolveForWorkspace(workspaceID string) ([]ResolvedPricingItem, error)

	PriceLLM(workspaceID string, model string, promptTokens, completionTokens int) (PriceResult, error)

	PriceTelephony(workspaceID string, durationSeconds float64) (PriceResult, error)

	PriceTelephonyChannel(workspaceID string, durationSeconds float64, channel string) (PriceResult, error)

	PriceWhatsApp(workspaceID string, templateCategory string) (PriceResult, error)
}

type pricer struct {
	repo             Repository
	planPricing      PlanPricingProvider
	llmFetcher       LLMPriceFetcher
	voiceRateFetcher VoiceCreditRateFetcher
	modelCostFetcher ModelCharCostFetcher

	resolvedCache    sync.Map
	resolvedCacheTTL time.Duration
}

type resolvedCacheEntry struct {
	items    []ResolvedPricingItem
	expireAt time.Time
}

var ResolvedCacheTTL = 30 * time.Second

func NewPricer(repo Repository, opts ...PricerOption) Pricer {
	p := &pricer{repo: repo, resolvedCacheTTL: ResolvedCacheTTL}
	for _, o := range opts {
		o(p)
	}
	return p
}

type PricerOption func(*pricer)

func WithLLMPriceFetcher(f LLMPriceFetcher) PricerOption {
	return func(p *pricer) { p.llmFetcher = f }
}

func WithVoiceCreditRateFetcher(f VoiceCreditRateFetcher) PricerOption {
	return func(p *pricer) { p.voiceRateFetcher = f }
}

func WithModelCharCostFetcher(f ModelCharCostFetcher) PricerOption {
	return func(p *pricer) { p.modelCostFetcher = f }
}

func WithPlanPricingProvider(pp PlanPricingProvider) PricerOption {
	return func(p *pricer) { p.planPricing = pp }
}

func (p *pricer) ResolveForWorkspace(workspaceID string) ([]ResolvedPricingItem, error) {

	if p.resolvedCacheTTL > 0 {
		if v, ok := p.resolvedCache.Load(workspaceID); ok {
			entry := v.(*resolvedCacheEntry)
			if time.Now().Before(entry.expireAt) {
				return entry.items, nil
			}
		}
	}

	defaults, err := p.repo.ListDefaultPricingItems()
	if err != nil {
		return nil, fmt.Errorf("failed to get default pricing: %w", err)
	}
	var planItems []PricingItem
	if workspaceID != "" && p.planPricing != nil {
		planItems, err = p.planPricing.ListForWorkspace(workspaceID)
		if err != nil {

			planItems = nil
		}
	}
	resolved := ResolvePricingLayers(defaults, planItems)

	if p.resolvedCacheTTL > 0 {
		p.resolvedCache.Store(workspaceID, &resolvedCacheEntry{
			items:    resolved,
			expireAt: time.Now().Add(p.resolvedCacheTTL),
		})
	}
	return resolved, nil
}

func (p *pricer) InvalidateResolvedCache(workspaceID string) {
	if workspaceID == "" {
		p.resolvedCache.Range(func(k, _ any) bool {
			p.resolvedCache.Delete(k)
			return true
		})
		return
	}
	p.resolvedCache.Delete(workspaceID)
}

func (p *pricer) PriceLLM(workspaceID string, model string, promptTokens, completionTokens int) (PriceResult, error) {
	if promptTokens <= 0 && completionTokens <= 0 {
		return PriceResult{}, nil
	}

	normalizedModel := model
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		normalizedModel = model[idx+1:]
	}

	resolved, err := p.ResolveForWorkspace(workspaceID)
	if err != nil {
		return PriceResult{}, err
	}

	inputPrice := findResolved(resolved, CategoryLLM, normalizedModel, "per_million_input_tokens")
	outputPrice := findResolved(resolved, CategoryLLM, normalizedModel, "per_million_output_tokens")

	if inputPrice <= 0 && outputPrice <= 0 && p.llmFetcher != nil {
		in, out, ferr := p.llmFetcher.FetchLLMPriceMicros(model)
		if ferr == nil {
			inputPrice, outputPrice = in, out
		}
	}

	var rawCost float64
	if promptTokens > 0 && inputPrice > 0 {
		rawCost += float64(promptTokens) / 1_000_000.0 * float64(inputPrice)
	}
	if completionTokens > 0 && outputPrice > 0 {
		rawCost += float64(completionTokens) / 1_000_000.0 * float64(outputPrice)
	}

	costMicros := int64(math.Ceil(rawCost))

	markupPct := findLLMMarkupPct(resolved)
	priceMicros := int64(math.Ceil(rawCost * (1 + markupPct)))

	return PriceResult{
		CostMicros:   costMicros,
		PriceMicros:  priceMicros,
		ProfitMicros: priceMicros - costMicros,
	}, nil
}

func (p *pricer) PriceTelephony(workspaceID string, durationSeconds float64) (PriceResult, error) {
	return p.PriceTelephonyChannel(workspaceID, durationSeconds, "")
}

func (p *pricer) PriceTelephonyChannel(workspaceID string, durationSeconds float64, channel string) (PriceResult, error) {
	if durationSeconds <= 0 {
		return PriceResult{}, nil
	}
	resolved, err := p.ResolveForWorkspace(workspaceID)
	if err != nil {
		return PriceResult{}, err
	}
	service := TelephonyServiceForChannel(channel)
	item := findResolvedItem(resolved, CategoryTelephony, service, "per_minute")
	if item == nil {

		return PriceResult{}, fmt.Errorf("%w: telephony/%s/per_minute (workspace %s)", ErrPricingItemNotFound, service, workspaceID)
	}
	if item.PriceMicros <= 0 {

		return PriceResult{}, nil
	}
	minutes := int64(math.Ceil(durationSeconds / 60.0))
	costMicros := minutes * item.CostMicros
	priceMicros := minutes * item.PriceMicros
	return PriceResult{
		CostMicros:   costMicros,
		PriceMicros:  priceMicros,
		ProfitMicros: priceMicros - costMicros,
	}, nil
}

func (p *pricer) PriceWhatsApp(workspaceID string, templateCategory string) (PriceResult, error) {
	resolved, err := p.ResolveForWorkspace(workspaceID)
	if err != nil {
		return PriceResult{}, err
	}
	service, err := NormalizeWhatsAppTemplateService(templateCategory)
	if err != nil {
		return PriceResult{}, err
	}
	item := findResolvedItem(resolved, CategoryWhatsApp, service, "per_message")
	if item == nil || item.PriceMicros <= 0 {
		return PriceResult{}, nil
	}
	return PriceResult{
		CostMicros:   item.CostMicros,
		PriceMicros:  item.PriceMicros,
		ProfitMicros: item.PriceMicros - item.CostMicros,
	}, nil
}

func findResolved(resolved []ResolvedPricingItem, category ServiceCategory, service, metric string) int64 {
	if item := findResolvedItem(resolved, category, service, metric); item != nil {
		return item.PriceMicros
	}
	return 0
}

func findResolvedItem(resolved []ResolvedPricingItem, category ServiceCategory, service, metric string) *ResolvedPricingItem {
	for i := range resolved {
		if resolved[i].Category == category && resolved[i].Service == service && resolved[i].Metric == metric {
			return &resolved[i]
		}
	}
	return nil
}

func findLLMMarkupPct(resolved []ResolvedPricingItem) float64 {
	for _, item := range resolved {
		if item.Category == CategoryLLM && item.MarkupPct > 0 {
			return item.MarkupPct
		}
	}
	return 0.20
}
