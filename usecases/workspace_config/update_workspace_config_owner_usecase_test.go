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

func (m *memWscRepo) GetIncludedUnofficialInstancesByWorkspaceIDs(context.Context, []string) (map[string]int, error) {
	return map[string]int{}, nil
}

type memWsOwner struct {
	ownerID string
}

func (m *memWsOwner) GetWorkspaceByID(id string) (*workspace.Workspace, error) {
	return &workspace.Workspace{ID: id, OwnerID: m.ownerID}, nil
}

func TestUpdateOwner_AutoCloseDefaultsAndClamp(t *testing.T) {
	repo := &memWscRepo{}
	uc := NewUpdateWorkspaceConfigOwnerUseCase(repo, &memWsOwner{ownerID: "owner-1"})

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
	uc := NewUpdateWorkspaceConfigOwnerUseCase(repo, &memWsOwner{ownerID: "owner-1"})
	enabled := false
	_, err := uc.Execute(context.Background(), "ws-1", "other", "employee", wsc.UpdateWorkspaceConfigOwnerInput{
		AutoCloseEnabled: &enabled,
	})
	require.ErrorIs(t, err, wsc.ErrForbidden)
}

func (m *memWscRepo) ListRoulettePolicies(context.Context) ([]wsc.RoulettePolicy, error) {
	return nil, nil
}

func TestUpdateOwner_RouletteClampsAndNormalizes(t *testing.T) {
	repo := &memWscRepo{}
	uc := NewUpdateWorkspaceConfigOwnerUseCase(repo, &memWsOwner{ownerID: "owner-1"})

	garbage := "not_a_mode"
	window := 9999
	minutes := 0
	rescue := true
	cfg, err := uc.Execute(context.Background(), "ws-1", "owner-1", "employee", wsc.UpdateWorkspaceConfigOwnerInput{
		RouletteMode:                &garbage,
		RouletteLastSeenWindowHours: &window,
		RouletteRescueEnabled:       &rescue,
		RouletteRescueAfterMinutes:  &minutes,
	})
	require.NoError(t, err)
	require.Equal(t, wsc.RouletteModeOnline, cfg.RouletteMode)
	require.Equal(t, wsc.MaxRouletteLastSeenWindowHours, cfg.RouletteLastSeenWindowHours)
	require.Equal(t, wsc.DefaultRouletteRescueAfterMinutes, cfg.RouletteRescueAfterMinutes)
	require.True(t, cfg.RouletteRescueEnabled)

	lastSeen := wsc.RouletteModeLastSeen
	window = 24
	minutes = 30
	cfg, err = uc.Execute(context.Background(), "ws-1", "owner-1", "employee", wsc.UpdateWorkspaceConfigOwnerInput{
		RouletteMode:                &lastSeen,
		RouletteLastSeenWindowHours: &window,
		RouletteRescueAfterMinutes:  &minutes,
	})
	require.NoError(t, err)
	require.Equal(t, wsc.RouletteModeLastSeen, cfg.RouletteMode)
	require.Equal(t, 24, cfg.RouletteLastSeenWindowHours)
	require.Equal(t, 30, cfg.RouletteRescueAfterMinutes)
	require.True(t, cfg.RouletteRescueActive())
}

func TestUpdateOwner_RoulettePartialUpdateLeavesTheRestAlone(t *testing.T) {
	repo := &memWscRepo{}
	uc := NewUpdateWorkspaceConfigOwnerUseCase(repo, &memWsOwner{ownerID: "owner-1"})

	lastSeen := wsc.RouletteModeLastSeen
	window := 12
	minutes := 45
	disabled := false
	_, err := uc.Execute(context.Background(), "ws-1", "owner-1", "employee", wsc.UpdateWorkspaceConfigOwnerInput{
		RouletteMode:                &lastSeen,
		RouletteLastSeenWindowHours: &window,
		RouletteRescueEnabled:       &disabled,
		RouletteRescueAfterMinutes:  &minutes,
	})
	require.NoError(t, err)

	skip := true
	cfg, err := uc.Execute(context.Background(), "ws-1", "owner-1", "employee", wsc.UpdateWorkspaceConfigOwnerInput{
		SkipAdminAssignment: &skip,
	})
	require.NoError(t, err)
	require.Equal(t, wsc.RouletteModeLastSeen, cfg.RouletteMode)
	require.Equal(t, 12, cfg.RouletteLastSeenWindowHours)
	require.Equal(t, 45, cfg.RouletteRescueAfterMinutes)
	require.False(t, cfg.RouletteRescueEnabled)
	require.True(t, cfg.SkipAdminAssignment)
}

func TestUpdateOwner_RouletteForbiddenForNonOwner(t *testing.T) {
	repo := &memWscRepo{}
	uc := NewUpdateWorkspaceConfigOwnerUseCase(repo, &memWsOwner{ownerID: "owner-1"})

	lastSeen := wsc.RouletteModeLastSeen
	_, err := uc.Execute(context.Background(), "ws-1", "someone-else", "employee", wsc.UpdateWorkspaceConfigOwnerInput{
		RouletteMode: &lastSeen,
	})
	require.ErrorIs(t, err, wsc.ErrForbidden)
	require.Nil(t, repo.cfg, "a forbidden update must not write anything")
}
