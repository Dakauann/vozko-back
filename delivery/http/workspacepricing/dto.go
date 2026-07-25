package workspacepricing

import (
	"time"

	pricingdomain "vozko/domain/workspace/workspace_pricing"
)

type PublicExchangeRateResponse struct {
	PriceMicros int64  `json:"priceMicros" example:"6000000"`
	Currency    string `json:"currency" example:"BRL"`
}

type PricingItemResponse struct {
	ID          string                        `json:"id"`
	WorkspaceID *string                       `json:"workspaceId"`
	Category    pricingdomain.ServiceCategory `json:"category"`
	Service     string                        `json:"service"`
	Metric      string                        `json:"metric"`
	CostMicros  int64                         `json:"costMicros"`
	PriceMicros int64                         `json:"priceMicros"`
	MarkupPct   float64                       `json:"markupPct"`
	Currency    string                        `json:"currency"`
	CreatedAt   time.Time                     `json:"createdAt"`
	UpdatedAt   time.Time                     `json:"updatedAt"`
}

type ResolvedPricingItemResponse struct {
	Category           pricingdomain.ServiceCategory `json:"category"`
	Service            string                        `json:"service"`
	Metric             string                        `json:"metric"`
	CostMicros         int64                         `json:"costMicros"`
	PriceMicros        int64                         `json:"priceMicros"`
	MarkupPct          float64                       `json:"markupPct"`
	Currency           string                        `json:"currency"`
	IsOverride         bool                          `json:"isOverride"`
	DefaultCostMicros  int64                         `json:"defaultCostMicros"`
	DefaultPriceMicros int64                         `json:"defaultPriceMicros"`
	DefaultMarkupPct   float64                       `json:"defaultMarkupPct"`
	OverrideID         *string                       `json:"overrideId,omitempty"`
	UpdatedAt          time.Time                     `json:"updatedAt"`
}

type PricingAuditEntryResponse struct {
	ID             string                        `json:"id"`
	WorkspaceID    *string                       `json:"workspaceId"`
	Category       pricingdomain.ServiceCategory `json:"category"`
	Service        string                        `json:"service"`
	Metric         string                        `json:"metric"`
	OldPriceMicros int64                         `json:"oldPriceMicros"`
	NewPriceMicros int64                         `json:"newPriceMicros"`
	Currency       string                        `json:"currency"`
	ChangedBy      string                        `json:"changedBy"`
	ChangedAt      time.Time                     `json:"changedAt"`
}

func toPublicExchangeRateResponse(item *pricingdomain.PricingItem) PublicExchangeRateResponse {
	if item == nil {
		return PublicExchangeRateResponse{}
	}
	return PublicExchangeRateResponse{
		PriceMicros: item.PriceMicros,
		Currency:    item.Currency,
	}
}

func toPricingItemResponse(item *pricingdomain.PricingItem) *PricingItemResponse {
	if item == nil {
		return nil
	}
	return &PricingItemResponse{
		ID:          item.ID,
		WorkspaceID: item.WorkspaceID,
		Category:    item.Category,
		Service:     item.Service,
		Metric:      item.Metric,
		CostMicros:  item.CostMicros,
		PriceMicros: item.PriceMicros,
		MarkupPct:   item.MarkupPct,
		Currency:    item.Currency,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}

func toPricingItemResponses(items []pricingdomain.PricingItem) []*PricingItemResponse {
	if items == nil {
		return nil
	}
	out := make([]*PricingItemResponse, len(items))
	for i := range items {
		out[i] = toPricingItemResponse(&items[i])
	}
	return out
}

func toResolvedPricingItemResponse(item *pricingdomain.ResolvedPricingItem) *ResolvedPricingItemResponse {
	if item == nil {
		return nil
	}
	return &ResolvedPricingItemResponse{
		Category:           item.Category,
		Service:            item.Service,
		Metric:             item.Metric,
		CostMicros:         item.CostMicros,
		PriceMicros:        item.PriceMicros,
		MarkupPct:          item.MarkupPct,
		Currency:           item.Currency,
		IsOverride:         item.IsOverride,
		DefaultCostMicros:  item.DefaultCostMicros,
		DefaultPriceMicros: item.DefaultPriceMicros,
		DefaultMarkupPct:   item.DefaultMarkupPct,
		OverrideID:         item.OverrideID,
		UpdatedAt:          item.UpdatedAt,
	}
}

func toResolvedPricingItemResponses(items []pricingdomain.ResolvedPricingItem) []*ResolvedPricingItemResponse {
	if items == nil {
		return nil
	}
	out := make([]*ResolvedPricingItemResponse, len(items))
	for i := range items {
		out[i] = toResolvedPricingItemResponse(&items[i])
	}
	return out
}

func toPricingAuditEntryResponse(entry *pricingdomain.PricingAuditEntry) *PricingAuditEntryResponse {
	if entry == nil {
		return nil
	}
	return &PricingAuditEntryResponse{
		ID:             entry.ID,
		WorkspaceID:    entry.WorkspaceID,
		Category:       entry.Category,
		Service:        entry.Service,
		Metric:         entry.Metric,
		OldPriceMicros: entry.OldPriceMicros,
		NewPriceMicros: entry.NewPriceMicros,
		Currency:       entry.Currency,
		ChangedBy:      entry.ChangedBy,
		ChangedAt:      entry.ChangedAt,
	}
}

func toPricingAuditEntryResponses(entries []pricingdomain.PricingAuditEntry) []*PricingAuditEntryResponse {
	if entries == nil {
		return nil
	}
	out := make([]*PricingAuditEntryResponse, len(entries))
	for i := range entries {
		out[i] = toPricingAuditEntryResponse(&entries[i])
	}
	return out
}
