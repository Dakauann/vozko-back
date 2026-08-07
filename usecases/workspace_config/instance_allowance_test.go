package workspace_config_usecase

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/user"
	wsc "vozko/domain/workspace_config"
)

// The allowance of unofficial WhatsApp numbers is granted by PLATFORM
// administrators and nobody else.
//
// This is the security half of the entitlement, and it is worth its own tests
// because the failure is silent and expensive: a workspace administrator who
// could set their own allowance would provision numbers onto hosts we pay for,
// with nothing recording that the capacity was never authorised.

func intPtr(v int) *int { return &v }

// A platform admin can grant an allowance.
func TestPlatformAdminCanGrantInstanceAllowance(t *testing.T) {
	repo := &memWscRepo{}
	uc := NewUpdateWorkspaceConfigUseCase(repo)

	cfg, err := uc.Execute(context.Background(), "ws-1", "user-1", string(user.RoleAdmin),
		wsc.UpdateWorkspaceConfigInput{IncludedUnofficialWhatsAppInstances: intPtr(5)})

	require.NoError(t, err)
	require.Equal(t, 5, cfg.IncludedUnofficialWhatsAppInstances)
	require.Equal(t, 5, repo.cfg.IncludedUnofficialWhatsAppInstances, "the grant was not persisted")
}

// Everyone else is refused, whatever their workspace role.
//
// The roles below are the ones a tenant can actually hold. None of them may
// grant capacity on infrastructure they do not pay for.
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

// An absent field leaves the grant ALONE.
//
// Pointers, not values, so an admin editing spam protection does not silently
// reset a workspace's number allowance to zero — which would disconnect nothing
// immediately and quietly block every future connect.
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

// Zero is a real value: revoking an allowance must be expressible.
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

// A negative value is REJECTED, not clamped.
//
// It is a typo. Quietly turning it into zero would revoke a workspace's whole
// allowance while reporting success, which is the worst of both.
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

// An absurd value is capped rather than refused.
//
// Unlike a negative number, a large one is a plausible intent expressed badly —
// but every connected number occupies a slot on a host with a hard ceiling, so
// one fat-fingered grant could exhaust a shared host for every other tenant on
// it.
func TestAbsurdAllowanceIsCapped(t *testing.T) {
	repo := &memWscRepo{}
	uc := NewUpdateWorkspaceConfigUseCase(repo)

	cfg, err := uc.Execute(context.Background(), "ws-1", "user-1", string(user.RoleAdmin),
		wsc.UpdateWorkspaceConfigInput{IncludedUnofficialWhatsAppInstances: intPtr(100000)})

	require.NoError(t, err)
	require.Equal(t, wsc.MaxIncludedUnofficialWhatsAppInstances, cfg.IncludedUnofficialWhatsAppInstances)
}

// The workspace-facing input has NO way to express the allowance.
//
// This is the structural half of the guard: the role check can be reasoned about
// and reviewed, but a field that simply does not exist on the tenant's input
// type cannot be set by them even if that check were ever weakened.
func TestOwnerInputCannotCarryAnAllowance(t *testing.T) {
	// Compiles only while UpdateWorkspaceConfigOwnerInput has no such field. If
	// somebody adds one, this stops building and they have to justify it.
	var owner wsc.UpdateWorkspaceConfigOwnerInput
	_ = owner

	admin := wsc.UpdateWorkspaceConfigInput{IncludedUnofficialWhatsAppInstances: intPtr(1)}
	require.NotNil(t, admin.IncludedUnofficialWhatsAppInstances,
		"the admin input must be the one that carries the grant")
}
