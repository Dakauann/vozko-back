package workspace_usecase

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/brand"
	"vozko/domain/notification"
	"vozko/domain/user"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

type inviteMemberUseCase struct {
	repo         workspace.Repository
	roleRepo     workspace.CustomRoleRepository
	userRepo     user.UserRepository
	deptRepo     wd.Repository
	emailService notification.EmailService
}

func NewInviteMemberUseCase(repo workspace.Repository, roleRepo workspace.CustomRoleRepository, userRepo user.UserRepository, deptRepo wd.Repository, emailService notification.EmailService) workspace.InviteMemberUseCase {
	return &inviteMemberUseCase{repo: repo, roleRepo: roleRepo, userRepo: userRepo, deptRepo: deptRepo, emailService: emailService}
}

func (uc *inviteMemberUseCase) Execute(inviterID, workspaceID, callerRole string, input workspace.InviteMemberInput) (*workspace.Invite, error) {
	email := strings.TrimSpace(strings.ToLower(input.Email))
	if email == "" {
		return nil, workspace.ErrInviteNotFound
	}

	if input.Role == workspace.RoleOwner {
		return nil, workspace.ErrInvalidRole
	}

	hasSystemRole := input.Role.IsValid() && input.Role != workspace.RoleOwner
	hasCustomRole := input.RoleID != ""

	if !hasSystemRole && !hasCustomRole {
		return nil, workspace.ErrInvalidRole
	}

	if hasCustomRole {
		input.Role = ""
	}

	isPlatformAdmin := callerRole == "admin"
	callerCanManageMembers := isPlatformAdmin

	if !isPlatformAdmin {
		member := mustBeMember(uc.repo, workspaceID, inviterID)
		if member == nil {
			return nil, workspace.ErrUnauthorized
		}

		if member.Role.CanManageMembers() {
			callerCanManageMembers = true
		} else {

			has, err := uc.repo.HasPermission(member.ID, workspace.ResourceMembers, workspace.ActionCreate)
			if err != nil {
				return nil, err
			}
			if !has {
				return nil, workspace.ErrInsufficientPermissions
			}
		}
	}

	if input.Role == workspace.RoleAdmin && !callerCanManageMembers {
		return nil, workspace.ErrInsufficientPermissions
	}

	inviter, _ := uc.userRepo.FindByID(inviterID)
	if inviter != nil && strings.EqualFold(inviter.Email, email) {
		return nil, workspace.ErrCannotInviteSelf
	}

	ws, err := uc.repo.GetWorkspaceByID(workspaceID)
	if err != nil {
		return nil, err
	}

	var permissions []workspace.PermissionEntry
	if hasCustomRole {
		role, err := uc.roleRepo.GetRoleByID(input.RoleID)
		if err != nil {
			return nil, workspace.ErrRoleNotFound
		}
		if role.WorkspaceID != workspaceID {
			return nil, workspace.ErrRoleNotFound
		}
		permissions = role.Permissions
	}

	existingUser, _ := uc.userRepo.FindByEmail(email)
	if existingUser != nil {
		existingMember, _ := uc.repo.GetMember(workspaceID, existingUser.ID)
		if existingMember != nil {
			return nil, workspace.ErrMemberAlreadyExists
		}
	}

	exists, err := uc.repo.PendingInviteExists(workspaceID, email)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, workspace.ErrInviteAlreadyExists
	}

	var validDeptIDs []string
	if len(input.DepartmentIDs) > 0 && uc.deptRepo != nil {
		allDepts, err := uc.deptRepo.ListDepartments(workspaceID)
		if err != nil {
			return nil, err
		}
		deptSet := make(map[string]struct{}, len(allDepts))
		for _, d := range allDepts {
			deptSet[d.ID] = struct{}{}
		}
		for _, id := range input.DepartmentIDs {
			if _, ok := deptSet[id]; !ok {
				return nil, workspace.ErrInvalidDepartment
			}
			validDeptIDs = append(validDeptIDs, id)
		}
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}

	invite := &workspace.Invite{
		ID:            uuid.New().String(),
		WorkspaceID:   workspaceID,
		InviterID:     inviterID,
		Email:         email,
		Role:          input.Role,
		RoleID:        input.RoleID,
		Status:        workspace.InviteStatusPending,
		Token:         hex.EncodeToString(tokenBytes),
		Permissions:   permissions,
		DepartmentIDs: validDeptIDs,
		ExpiresAt:     time.Now().Add(7 * 24 * time.Hour),
	}

	if err := uc.repo.CreateInvite(invite); err != nil {
		return nil, err
	}

	inviterEmail := ""
	if inviter, err := uc.userRepo.FindByID(inviterID); err == nil && inviter != nil {
		inviterEmail = inviter.Email
	}

	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = brand.Active().SiteURL
	}
	acceptURL := frontendURL + "/invite?token=" + invite.Token
	loginURL := frontendURL + "/login"

	go func() {
		subject := "Convite para Área de Trabalho: " + ws.Name + ", " + brand.Active().Name
		if err := uc.emailService.SendTemplate(email, subject, "workspace_invite.html", map[string]interface{}{
			"Email":         email,
			"InviterEmail":  inviterEmail,
			"WorkspaceName": ws.Name,
			"Role":          string(input.Role),
			"Token":         invite.Token,
			"AcceptURL":     acceptURL,
			"LoginURL":      loginURL,
		}); err != nil {
			log.Printf("WORKSPACE INVITE EMAIL FAILED for %s: %v", email, err)
		}
	}()

	invite.WorkspaceName = ws.Name
	invite.InviterEmail = inviterEmail
	return invite, nil
}
