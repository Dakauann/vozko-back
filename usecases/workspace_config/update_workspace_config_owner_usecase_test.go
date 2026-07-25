package workspace_config_usecase

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/workspace"
	wsc "vozko/domain/workspace_config"
)

type memWscRepo struct {
	cfg *wsc.WorkspaceConfig
}

func (m *memWscRepo) GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error) {
	if m.cfg == nil {
		return &wsc.WorkspaceConfig{
			WorkspaceID:             workspaceID,
			AutoCloseEnabled:        wsc.DefaultAutoCloseEnabled,
			AutoCloseIdleAfterHours: wsc.DefaultAutoCloseIdleAfterHours,
		}, nil
	}
	cp := *m.cfg
	return &cp, nil
}
func (m *memWscRepo) Upsert(ctx context.Context, cfg *wsc.WorkspaceConfig) error {
	cp := *cfg
	m.cfg = &cp
	return nil
}
func (m *memWscRepo) EnsureExists(ctx context.Context, workspaceID string) error { return nil }

type memWsOwner struct {
	ownerID string
}

func (m *memWsOwner) GetWorkspaceByID(id string) (*workspace.Workspace, error) {
	return &workspace.Workspace{ID: id, OwnerID: m.ownerID}, nil
}

func TestUpdateOwner_AutoCloseDefaultsAndClamp(t *testing.T) {
	repo := &memWscRepo{}
	uc := NewUpdateWorkspaceConfigOwnerUseCase(repo, &memWsOwner{ownerID: "owner-1"}, nil)

	enabled := true
	hours := 200
	cfg, err := uc.Execute(context.Background(), "ws-1", "owner-1", "employee", wsc.UpdateWorkspaceConfigOwnerInput{
		AutoCloseEnabled:        &enabled,
		AutoCloseIdleAfterHours: &hours,
	})
	require.NoError(t, err)
	require.True(t, cfg.AutoCloseEnabled)
	require.Equal(t, wsc.MaxAutoCloseIdleAfterHours, cfg.AutoCloseIdleAfterHours)

	disabled := false
	hours = 6
	cfg, err = uc.Execute(context.Background(), "ws-1", "owner-1", "employee", wsc.UpdateWorkspaceConfigOwnerInput{
		AutoCloseEnabled:        &disabled,
		AutoCloseIdleAfterHours: &hours,
	})
	require.NoError(t, err)
	require.False(t, cfg.AutoCloseEnabled)
	require.Equal(t, 6, cfg.AutoCloseIdleAfterHours)
}

func TestUpdateOwner_ForbiddenNonOwner(t *testing.T) {
	repo := &memWscRepo{}
	uc := NewUpdateWorkspaceConfigOwnerUseCase(repo, &memWsOwner{ownerID: "owner-1"}, nil)
	enabled := false
	_, err := uc.Execute(context.Background(), "ws-1", "other", "employee", wsc.UpdateWorkspaceConfigOwnerInput{
		AutoCloseEnabled: &enabled,
	})
	require.ErrorIs(t, err, wsc.ErrForbidden)
}
