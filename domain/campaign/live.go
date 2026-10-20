package campaign

import "context"

type LiveCampaign struct {
	ID           string
	WorkspaceID  string
	DepartmentID string
	Status       Status
}

type ProgressUseCase interface {
	Execute(ctx context.Context, live LiveCampaign) (*Progress, error)
}

type TimelineUseCase interface {
	Execute(ctx context.Context, live LiveCampaign, bucket TimelineBucket) (*Timeline, error)
}
