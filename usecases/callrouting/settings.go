package callrouting_usecase

import (
	"context"

	"vozko/domain/callrouting"
)

type RoutingSettings struct {
	settings callrouting.SettingsRepository
	music    callrouting.HoldMusicLibrary
}

var _ WorkspaceHoldMusic = (*RoutingSettings)(nil)

func NewRoutingSettings(settings callrouting.SettingsRepository, music callrouting.HoldMusicLibrary) *RoutingSettings {
	return &RoutingSettings{settings: settings, music: music}
}

func (s *RoutingSettings) Get(ctx context.Context, workspaceID string) (callrouting.Settings, error) {
	return s.settings.Get(ctx, workspaceID)
}

func (s *RoutingSettings) Save(ctx context.Context, settings callrouting.Settings) (callrouting.Settings, error) {
	if settings.WorkspaceID == "" {
		return callrouting.Settings{}, callrouting.ErrWorkspaceRequired
	}
	if settings.HoldMusic.IsZero() {
		settings.HoldMusic = callrouting.DefaultSettings(settings.WorkspaceID).HoldMusic
	}
	if _, err := s.music.PCM(ctx, settings.WorkspaceID, settings.HoldMusic); err != nil {
		return callrouting.Settings{}, err
	}
	if err := s.settings.Save(ctx, settings); err != nil {
		return callrouting.Settings{}, err
	}
	return settings, nil
}

func (s *RoutingSettings) HoldMusicFor(ctx context.Context, workspaceID string) callrouting.HoldMusicRef {
	settings, err := s.settings.Get(ctx, workspaceID)
	if err != nil {
		return callrouting.DefaultSettings(workspaceID).HoldMusic
	}
	return settings.HoldMusic
}
