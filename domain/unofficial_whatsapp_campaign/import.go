package unofficial_whatsapp_campaign

import (
	"context"

	"vozko/domain/campaign"
	"vozko/domain/lead"
	uw "vozko/domain/unofficial_whatsapp"
)

func NormalizeTarget(raw string) string {
	return lead.NormalizeNumber(lead.NormalizeRawNumber(raw))
}

type ImportRequest struct {
	WorkspaceID string
	MediaID     string
	Message     MessageSpec
	Mapping     campaign.ColumnMapping
}

type ImportPreview struct {
	campaign.ImportResult
}

func (p *ImportPreview) Targets() []TargetInput {
	out := make([]TargetInput, 0, len(p.Rows))
	for _, row := range p.Rows {
		out = append(out, TargetInput{Number: row.Number, Name: row.Name, Variables: row.Variables})
	}
	return out
}

type ImportPreviewUseCase interface {
	Preview(ctx context.Context, req ImportRequest) (*ImportPreview, error)
}

type CampaignInstanceUseCase interface {
	Usable(ctx context.Context, workspaceID string, scope uw.DepartmentScope, instanceID string) (*uw.Instance, error)
}

type CampaignActionUseCase interface {
	Act(ctx context.Context, workspaceID string, scope uw.DepartmentScope, campaignID string, action campaign.Action) (*Campaign, error)
}
