package whatsapp_campaign_usecase

import (
	"strings"

	"vozko/domain/balance"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type getSummaryUseCase struct {
	aggregator wce.SummaryAggregator
	// chargeAgg is optional. When set, Envios + by-category volume come from the
	// balance ledger (true charged sends) instead of current entry status, so a
	// campaign reset cannot erase billed reality.
	chargeAgg balance.WhatsAppChargeAggregator
}

// NewGetSummaryUseCase builds the workspace-level "disparos" summary usecase.
// chargeAgg may be nil (falls back to entry-status counts for Envios).
func NewGetSummaryUseCase(
	aggregator wce.SummaryAggregator,
	chargeAgg balance.WhatsAppChargeAggregator,
) wc.GetSummaryUseCase {
	return &getSummaryUseCase{aggregator: aggregator, chargeAgg: chargeAgg}
}

func (uc *getSummaryUseCase) Execute(filter wce.WorkspaceSummaryFilter) (*wc.CampaignMetrics, error) {
	counts, err := uc.aggregator.CountByStatusForWorkspace(filter)
	if err != nil {
		return nil, err
	}
	metrics := wc.NewCampaignMetrics(counts)

	// Prefer ledger for billed volume (Envios + type split). Delivery funnel
	// (entregues/lidas/falhas) stays entry-based: that is operational state.
	if uc.chargeAgg != nil && strings.TrimSpace(filter.WorkspaceID) != "" {
		stats, chargeErr := uc.chargeAgg.AggregateWhatsAppTemplateCharges(balance.WhatsAppChargeFilter{
			WorkspaceID:   filter.WorkspaceID,
			DepartmentIDs: filter.DepartmentIDs,
			CampaignType:  filter.Type,
			From:          filter.CreatedFrom,
			To:            filter.CreatedTo,
		})
		if chargeErr == nil && stats != nil {
			metrics.Dispatches = stats.NetDispatches
			if stats.Marketing > 0 || stats.Utility > 0 || stats.Authentication > 0 || stats.NetDispatches > 0 {
				metrics.ByCategory = &wc.CategoryDispatches{
					Marketing:      stats.Marketing,
					Utility:        stats.Utility,
					Authentication: stats.Authentication,
				}
			}
			return metrics, nil
		}
		// Fall through to entry-status category if ledger unavailable.
	}

	byCat, err := uc.aggregator.CountDispatchesByCategoryForWorkspace(filter)
	if err != nil {
		return metrics, nil
	}
	if len(byCat) == 0 {
		return metrics, nil
	}

	cd := &wc.CategoryDispatches{}
	for cat, n := range byCat {
		switch strings.ToUpper(strings.TrimSpace(cat)) {
		case "MARKETING":
			cd.Marketing += n
		case "UTILITY":
			cd.Utility += n
		case "AUTHENTICATION":
			cd.Authentication += n
		}
	}
	if cd.Marketing > 0 || cd.Utility > 0 || cd.Authentication > 0 {
		metrics.ByCategory = cd
	}
	return metrics, nil
}
