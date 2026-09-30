package callrouting

import "context"

type Settings struct {
	WorkspaceID string
	HoldMusic   HoldMusicRef
}

func DefaultSettings(workspaceID string) Settings {
	return Settings{WorkspaceID: workspaceID, HoldMusic: HoldMusicRef{PresetID: DefaultHoldPreset}}
}

type SettingsRepository interface {
	Get(ctx context.Context, workspaceID string) (Settings, error)
	Save(ctx context.Context, settings Settings) error
}
