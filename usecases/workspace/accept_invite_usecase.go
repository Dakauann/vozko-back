package workspace_usecase

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

type acceptInviteUseCase struct {
	repo     workspace.Repository
	deptRepo wd.Repository
}

func NewAcceptInviteUseCase(repo workspace.Repository, deptRepo wd.Repository) workspace.AcceptInviteUseCase {
	return &acceptInviteUseCase{repo: repo, deptRepo: deptRepo}
}

func (uc *acceptInviteUseCase) Execute(userID, userEmail, token string) (*workspace.Member, error) {
	invite, err := uc.repo.GetInviteByToken(token)
	if err != nil {
		return nil, err
	}

	if invite.Status != workspace.InviteStatusPending {
		return nil, workspace.ErrInviteAlreadyProcessed
	}

	if time.Now().After(invite.ExpiresAt) {
		_ = uc.repo.UpdateInviteStatus(invite.ID, workspace.InviteStatusExpired)
		return nil, workspace.ErrInviteExpired
	}

	if !strings.EqualFold(strings.TrimSpace(userEmail), strings.TrimSpace(invite.Email)) {
		return nil, workspace.ErrUnauthorized
	}

	existing, err := uc.repo.GetMember(invite.WorkspaceID, userID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		_ = uc.repo.UpdateInviteStatus(invite.ID, workspace.InviteStatusAccepted)
		return existing, workspace.ErrMemberAlreadyExists
	}

	member := &workspace.Member{
		ID:          uuid.New().String(),
		WorkspaceID: invite.WorkspaceID,
		UserID:      userID,
		Role:        invite.Role,
		RoleID:      invite.RoleID,
	}
	if err := uc.repo.AddMember(member); err != nil {
		return nil, err
	}

	if len(invite.Permissions) > 0 {
		permissions := make([]*workspace.Permission, len(invite.Permissions))
		for i, pe := range invite.Permissions {
			permissions[i] = &workspace.Permission{
				ID:       uuid.New().String(),
				MemberID: member.ID,
				Resource: pe.Resource,
				Action:   pe.Action,
			}
		}
		_ = uc.repo.SetPermissions(member.ID, permissions)
	}

	if len(invite.DepartmentIDs) > 0 && uc.deptRepo != nil {
		for _, deptID := range invite.DepartmentIDs {
			dm := &wd.DepartmentMember{
				ID:           uuid.New().String(),
				DepartmentID: deptID,
				MemberID:     member.ID,
			}
			_ = uc.deptRepo.AddMember(dm)
		}
	}

	if err := uc.repo.UpdateInviteStatus(invite.ID, workspace.InviteStatusAccepted); err != nil {
		return nil, err
	}

	return member, nil
}
