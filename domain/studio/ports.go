package studio

import "context"

type ListQuery struct {
	WorkspaceID string
	Kind        Kind
	Limit       int
	Offset      int
}

type Repository interface {
	Create(ctx context.Context, p *Project) error
	Get(ctx context.Context, workspaceID, id string) (*Project, error)
	List(ctx context.Context, q ListQuery) ([]Summary, int64, error)
	Save(ctx context.Context, p *Project, expectedVersion int64) error
	Archive(ctx context.Context, workspaceID, id string) error
}

type PresetRepository interface {
	CreatePreset(ctx context.Context, p *AnimationPreset) error
	ListPresets(ctx context.Context, workspaceID string) ([]AnimationPreset, error)
	GetPreset(ctx context.Context, workspaceID, id string) (*AnimationPreset, error)
	SavePreset(ctx context.Context, p *AnimationPreset) error
	DeletePreset(ctx context.Context, workspaceID, id string) error
}

type CapabilityRepository interface {
	Upsert(ctx context.Context, report CapabilityReport) error
}
