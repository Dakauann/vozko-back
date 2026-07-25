package workspace_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/affiliate"
	"vozko/domain/workspace"
	wsc "vozko/domain/workspace_config"
)

type ensureDefaultWorkspaceUseCase struct {
	repo          workspace.Repository
	configRepo    wsc.Repository
	trackReferral affiliate.TrackReferralUseCase
}

func NewEnsureDefaultWorkspaceUseCase(
	repo workspace.Repository,
	configRepo wsc.Repository,
	trackReferral affiliate.TrackReferralUseCase,
) workspace.EnsureDefaultWorkspaceUseCase {
	return &ensureDefaultWorkspaceUseCase{
		repo:          repo,
		configRepo:    configRepo,
		trackReferral: trackReferral,
	}
}

func (uc *ensureDefaultWorkspaceUseCase) Execute(userID, email, referralCode string) (*workspace.Workspace, error) {
	existing, err := uc.repo.GetDefaultWorkspace(userID)
	if err != nil {
		return nil, err
	}
	if existing != nil {

		if uc.configRepo != nil {
			_ = uc.configRepo.EnsureExists(context.Background(), existing.ID)
		}

		return existing, nil
	}

	ws := &workspace.Workspace{
		ID:        uuid.New().String(),
		OwnerID:   userID,
		Name:      email,
		IsDefault: true,
	}
	if err := uc.repo.CreateWorkspace(ws); err != nil {
		return nil, err
	}

	member := &workspace.Member{
		ID:          uuid.New().String(),
		WorkspaceID: ws.ID,
		UserID:      userID,
		Role:        workspace.RoleOwner,
	}
	if err := uc.repo.AddMember(member); err != nil {
		return nil, err
	}

	if uc.configRepo != nil {
		if err := uc.configRepo.EnsureExists(context.Background(), ws.ID); err != nil {
			fmt.Printf("ensure default workspace: failed to create config for workspace %s: %v\n", ws.ID, err)
		}
	}

	code := strings.TrimSpace(referralCode)
	if code != "" && uc.trackReferral != nil {
		if _, refErr := uc.trackReferral.Execute(context.Background(), affiliate.TrackReferralInput{
			Code:                 code,
			WorkspaceID:          ws.ID,
			WorkspaceOwnerUserID: userID,
		}); refErr != nil {
			log.Printf("[ensure-default-workspace] referral tracking failed user=%s ws=%s code=%s: %v", userID, ws.ID, code, refErr)
		}
	}

	return ws, nil
}
