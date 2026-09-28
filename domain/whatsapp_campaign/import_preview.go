package whatsapp_campaign

import (
	"context"

	"vozko/domain/campaign"
)

type ImportRequest struct {
	WorkspaceID string
	MediaID     string
	TemplateID  string
	Mapping     campaign.ColumnMapping
}

type ImportPreview struct {
	campaign.ImportResult
	UnitCostMicros int64 `json:"unitCostMicros"`
	CostMicros     int64 `json:"costMicros"`
	BalanceMicros  int64 `json:"balanceMicros"`
	Affordable     bool  `json:"affordable"`
}

func (p *ImportPreview) PhoneInputs() []PhoneInput {
	out := make([]PhoneInput, 0, len(p.Rows))
	for _, row := range p.Rows {
		out = append(out, PhoneInput{Number: row.Number, Name: row.Name, Variables: row.Variables})
	}
	return out
}

type ImportPreviewUseCase interface {
	Preview(ctx context.Context, req ImportRequest) (*ImportPreview, error)
}
