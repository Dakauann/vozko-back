package buttonaction

import "context"

type TemplateSettingsStore interface {
	Get(ctx context.Context, workspaceID string, ref TemplateRef) (TemplateSettings, error)
	Save(ctx context.Context, s *TemplateSettings, expectedVersion int64) error
}

type CampaignRoutesStore interface {
	Get(ctx context.Context, workspaceID, campaignID string) (CampaignRoutes, error)
	Save(ctx context.Context, r *CampaignRoutes, expectedVersion int64) error
}
