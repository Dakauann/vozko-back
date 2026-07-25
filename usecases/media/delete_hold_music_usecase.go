package media_usecase

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"vozko/domain/media"
	wsc "vozko/domain/workspace_config"
)

// deleteHoldMusicUseCase removes a workspace's OWN hold music track, freeing a
// quota slot. Only hold_music media are deletable here (other types may be
// referenced by workflows/campaigns; there is no reference counting yet). If the
// deleted track was the workspace's selected hold music, the selection resets to
// the system default; the live hold provider needs no invalidation because it
// re-reads the selection per hold and a vanished media falls back safely anyway.
type deleteHoldMusicUseCase struct {
	repo    media.MediaRepository
	configs wsc.Repository
}

func NewDeleteHoldMusicUseCase(repo media.MediaRepository, configs wsc.Repository) media.DeleteHoldMusicUseCase {
	return &deleteHoldMusicUseCase{repo: repo, configs: configs}
}

func (uc *deleteHoldMusicUseCase) DeleteHoldMusic(ctx context.Context, workspaceID, mediaID string) error {
	if workspaceID == "" || mediaID == "" {
		return media.ErrMediaNotFound
	}
	m, err := uc.repo.GetMediaByID(mediaID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return media.ErrMediaNotFound
		}
		return err
	}
	// Cross-workspace probes get the same answer as a missing row: no existence
	// leak, no cross-tenant deletes.
	if m == nil || m.WorkspaceID != workspaceID {
		return media.ErrMediaNotFound
	}
	if m.Type != media.MediaTypeHoldMusic {
		return media.ErrNotHoldMusic
	}

	if err := uc.repo.DeleteMedia(mediaID); err != nil {
		return err
	}

	// Best effort selection reset: even if this fails, the hold path already
	// treats a missing media as "use the fallback audio", so nothing breaks.
	if uc.configs != nil {
		if cfg, cErr := uc.configs.GetByWorkspaceID(ctx, workspaceID); cErr == nil && cfg != nil && cfg.HoldMusicTrack == mediaID {
			cfg.HoldMusicTrack = ""
			_ = uc.configs.Upsert(ctx, cfg)
		}
	}
	return nil
}
