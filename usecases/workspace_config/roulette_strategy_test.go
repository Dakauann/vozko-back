package workspace_config_usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	ia "vozko/domain/inbox_assignment"
	wsc "vozko/domain/workspace_config"
)

func ownerUpdate(t *testing.T, repo *memWscRepo, input wsc.UpdateWorkspaceConfigOwnerInput) (*wsc.WorkspaceConfig, error) {
	t.Helper()
	uc := NewUpdateWorkspaceConfigOwnerUseCase(repo, &memWsOwner{ownerID: "owner-1", members: []string{"owner-1", "ana", "bob"}})
	return uc.Execute(context.Background(), "ws-1", "owner-1", "employee", input)
}

func strategyInput(s string) *string { return &s }

func recipientsInput(list ...ia.Recipient) *[]ia.Recipient { return &list }

func TestUpdateOwner_SavesAPriorityRouletteWithItsRecipients(t *testing.T) {
	repo := &memWscRepo{}
	cfg, err := ownerUpdate(t, repo, wsc.UpdateWorkspaceConfigOwnerInput{
		RouletteStrategy:   strategyInput("priority"),
		RouletteRecipients: recipientsInput(ia.Recipient{UserID: "bob", Tier: 2, Weight: 1}, ia.Recipient{UserID: " ana ", Tier: 1, Weight: 3}),
	})
	require.NoError(t, err)
	require.Equal(t, ia.StrategyPriority, cfg.RouletteStrategy)
	require.Equal(t, []ia.Recipient{{UserID: "ana", Tier: 1, Weight: 3}, {UserID: "bob", Tier: 2, Weight: 1}}, repo.cfg.RouletteRecipients)
}

func TestUpdateOwner_RefusesAListStrategyWithoutRecipients(t *testing.T) {
	repo := &memWscRepo{}
	_, err := ownerUpdate(t, repo, wsc.UpdateWorkspaceConfigOwnerInput{RouletteStrategy: strategyInput("weighted")})
	require.ErrorIs(t, err, ia.ErrRecipientsRequired)
	require.Nil(t, repo.cfg)
}

func TestUpdateOwner_RefusesEmptyingTheListOfTheCurrentStrategy(t *testing.T) {
	repo := &memWscRepo{cfg: &wsc.WorkspaceConfig{WorkspaceID: "ws-1", RouletteStrategy: ia.StrategyPriority, RouletteRecipients: []ia.Recipient{{UserID: "ana", Tier: 1, Weight: 1}}}}
	_, err := ownerUpdate(t, repo, wsc.UpdateWorkspaceConfigOwnerInput{RouletteRecipients: recipientsInput()})
	require.ErrorIs(t, err, ia.ErrRecipientsRequired)
}

func TestUpdateOwner_RefusesRecipientsOutsideTheWorkspace(t *testing.T) {
	repo := &memWscRepo{}
	_, err := ownerUpdate(t, repo, wsc.UpdateWorkspaceConfigOwnerInput{
		RouletteStrategy:   strategyInput("priority"),
		RouletteRecipients: recipientsInput(ia.Recipient{UserID: "stranger", Tier: 1, Weight: 1}),
	})
	require.ErrorIs(t, err, ia.ErrRecipientNotMember)
	require.Nil(t, repo.cfg)
}

func TestUpdateOwner_RefusesWhenMembershipCannotBeRead(t *testing.T) {
	repo := &memWscRepo{}
	uc := NewUpdateWorkspaceConfigOwnerUseCase(repo, &memWsOwner{ownerID: "owner-1", membersErr: errors.New("db down")})
	_, err := uc.Execute(context.Background(), "ws-1", "owner-1", "employee", wsc.UpdateWorkspaceConfigOwnerInput{
		RouletteStrategy:   strategyInput("priority"),
		RouletteRecipients: recipientsInput(ia.Recipient{UserID: "ana", Tier: 1, Weight: 1}),
	})
	require.Error(t, err)
	require.Nil(t, repo.cfg)
}

func TestUpdateOwner_RefusesAnUnknownStrategy(t *testing.T) {
	_, err := ownerUpdate(t, &memWscRepo{}, wsc.UpdateWorkspaceConfigOwnerInput{RouletteStrategy: strategyInput("first_come")})
	require.ErrorIs(t, err, ia.ErrInvalidStrategy)
}

func TestUpdateOwner_RefusesInvalidRecipients(t *testing.T) {
	_, err := ownerUpdate(t, &memWscRepo{}, wsc.UpdateWorkspaceConfigOwnerInput{
		RouletteRecipients: recipientsInput(ia.Recipient{UserID: "ana", Tier: 1, Weight: 0}),
	})
	require.ErrorIs(t, err, ia.ErrRecipientInvalid)
}

func TestUpdateOwner_Capacity(t *testing.T) {
	repo := &memWscRepo{}
	limit := 8
	cfg, err := ownerUpdate(t, repo, wsc.UpdateWorkspaceConfigOwnerInput{RouletteMaxOpenPerMember: &limit})
	require.NoError(t, err)
	require.Equal(t, 8, cfg.RouletteMaxOpenPerMember)

	none := 0
	cfg, err = ownerUpdate(t, repo, wsc.UpdateWorkspaceConfigOwnerInput{RouletteMaxOpenPerMember: &none})
	require.NoError(t, err)
	require.Zero(t, cfg.RouletteMaxOpenPerMember)

	for _, bad := range []int{-1, ia.MaxOpenPerMember + 1} {
		_, err = ownerUpdate(t, repo, wsc.UpdateWorkspaceConfigOwnerInput{RouletteMaxOpenPerMember: &bad})
		require.ErrorIs(t, err, ia.ErrInvalidCapacity)
	}
}

func TestUpdateOwner_SwitchingStrategyKeepsTheList(t *testing.T) {
	repo := &memWscRepo{}
	_, err := ownerUpdate(t, repo, wsc.UpdateWorkspaceConfigOwnerInput{
		RouletteStrategy:   strategyInput("weighted"),
		RouletteRecipients: recipientsInput(ia.Recipient{UserID: "ana", Tier: 1, Weight: 4}),
	})
	require.NoError(t, err)

	cfg, err := ownerUpdate(t, repo, wsc.UpdateWorkspaceConfigOwnerInput{RouletteStrategy: strategyInput("least_busy")})
	require.NoError(t, err)
	require.Equal(t, ia.StrategyLeastBusy, cfg.RouletteStrategy)
	require.Len(t, cfg.RouletteRecipients, 1)
}
