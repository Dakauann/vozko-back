package workspaceaddon

import (
	"time"

	workspaceaddondomain "vozko/domain/workspace/workspace_addon"
)

type customerAddonDefinition struct {
	ID                 string     `json:"id"`
	Key                string     `json:"key"`
	Name               string     `json:"name"`
	Description        string     `json:"description"`
	EntitlementKind    string     `json:"entitlementKind"`
	UnitsPerQuantity   int        `json:"unitsPerQuantity"`
	MonthlyPriceMicros int64      `json:"monthlyPriceMicros"`
	AnnualPriceMicros  int64      `json:"annualPriceMicros"`
	IsActive           bool       `json:"isActive"`
	IsGloballyVisible  bool       `json:"isGloballyVisible"`
	ArchivedAt         *time.Time `json:"archivedAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

func toCustomerAddonDefinitions(defs []*workspaceaddondomain.AddonDefinition) []customerAddonDefinition {
	out := make([]customerAddonDefinition, 0, len(defs))
	for _, d := range defs {
		out = append(out, customerAddonDefinition{
			ID:                 d.ID,
			Key:                d.Key,
			Name:               d.Name,
			Description:        d.Description,
			EntitlementKind:    string(d.EntitlementKind),
			UnitsPerQuantity:   d.UnitsPerQuantity,
			MonthlyPriceMicros: d.MonthlyPriceMicros,
			AnnualPriceMicros:  d.AnnualPriceMicros,
			IsActive:           d.IsActive,
			IsGloballyVisible:  d.IsGloballyVisible,
			ArchivedAt:         d.ArchivedAt,
			CreatedAt:          d.CreatedAt,
			UpdatedAt:          d.UpdatedAt,
		})
	}
	return out
}
