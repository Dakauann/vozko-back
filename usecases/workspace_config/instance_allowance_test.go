package workspace_config_usecase

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/user"
	wsc "vozko/domain/workspace_config"
)

func intPtr(v int) *int { return &v }

func TestPlatformAdminCanGrantInstanceAllowance(t *testing.T) {
	repo := &memWscRepo{}
	uc := NewUpdateWorkspaceConfigUseCase(repo)

	cfg, err := uc.Execute(context.Background(), "ws-1", "user-1", string(user.RoleAdmin),
		wsc.UpdateWorkspaceConfigInput{IncludedUnofficialWhatsAppInstances: intPtr(5)})

	require.NoError(t, err)
	require.Equal(t, 5, cfg.IncludedUnofficialWhatsAppInstances)
	require.Equal(t, 5, repo.cfg.IncludedUnofficialWhatsAppInstances, "the grant was not persisted")
}

func TestNonPlatformAdminsCannotGrantInstanceAllowance(t *testing.T) {
	for _, role := range []string{"user", "owner", "manager", "attendant", ""} {
		t.Run("role="+role, func(t *testing.T) {
			repo := &memWscRepo{}
			uc := NewUpdateWorkspaceConfigUseCase(repo)

			_, err := uc.Execute(context.Background(), "ws-1", "user-1", role,
				wsc.UpdateWorkspaceConfigInput{IncludedUnofficialWhatsAppInstances: intPtr(50)})

			require.ErrorIs(t, err, wsc.ErrUnauthorized)
			require.Nil(t, repo.cfg, "a refused request must not write anything")
		})
	}
}

func TestUnsentAllowanceIsNotOverwritten(t *testing.T) {
	repo := &memWscRepo{cfg: &wsc.WorkspaceConfig{
		WorkspaceID:                         "ws-1",
		IncludedUnofficialWhatsAppInstances: 8,
	}}
	uc := NewUpdateWorkspaceConfigUseCase(repo)

	cfg, err := uc.Execute(context.Background(), "ws-1", "user-1", string(user.RoleAdmin),
		wsc.UpdateWorkspaceConfigInput{CampaignSpamProtectionDays: intPtr(7)})

	require.NoError(t, err)
	require.Equal(t, 8, cfg.IncludedUnofficialWhatsAppInstances,
		"an unrelated edit reset the allowance")
}

func TestAllowanceCanBeRevokedToZero(t *testing.T) {
	repo := &memWscRepo{cfg: &wsc.WorkspaceConfig{
		WorkspaceID:                         "ws-1",
		IncludedUnofficialWhatsAppInstances: 4,
	}}
	uc := NewUpdateWorkspaceConfigUseCase(repo)

	cfg, err := uc.Execute(context.Background(), "ws-1", "user-1", string(user.RoleAdmin),
		wsc.UpdateWorkspaceConfigInput{IncludedUnofficialWhatsAppInstances: intPtr(0)})

	require.NoError(t, err)
	require.Equal(t, 0, cfg.IncludedUnofficialWhatsAppInstances)
}

func TestNegativeAllowanceIsRejected(t *testing.T) {
	repo := &memWscRepo{cfg: &wsc.WorkspaceConfig{
		WorkspaceID:                         "ws-1",
		IncludedUnofficialWhatsAppInstances: 4,
	}}
	uc := NewUpdateWorkspaceConfigUseCase(repo)

	_, err := uc.Execute(context.Background(), "ws-1", "user-1", string(user.RoleAdmin),
		wsc.UpdateWorkspaceConfigInput{IncludedUnofficialWhatsAppInstances: intPtr(-1)})

	require.ErrorIs(t, err, wsc.ErrInvalidIncludedInstances)
	require.Equal(t, 4, repo.cfg.IncludedUnofficialWhatsAppInstances,
		"a rejected request must leave the existing grant untouched")
}

func TestAbsurdAllowanceIsCapped(t *testing.T) {
	repo := &memWscRepo{}
	uc := NewUpdateWorkspaceConfigUseCase(repo)

	cfg, err := uc.Execute(context.Background(), "ws-1", "user-1", string(user.RoleAdmin),
		wsc.UpdateWorkspaceConfigInput{IncludedUnofficialWhatsAppInstances: intPtr(100000)})

	require.NoError(t, err)
	require.Equal(t, wsc.MaxIncludedUnofficialWhatsAppInstances, cfg.IncludedUnofficialWhatsAppInstances)
}

func TestOwnerInputCannotCarryAnAllowance(t *testing.T) {
	var owner wsc.UpdateWorkspaceConfigOwnerInput
	_ = owner

	admin := wsc.UpdateWorkspaceConfigInput{IncludedUnofficialWhatsAppInstances: intPtr(1)}
	require.NotNil(t, admin.IncludedUnofficialWhatsAppInstances,
		"the admin input must be the one that carries the grant")
}
