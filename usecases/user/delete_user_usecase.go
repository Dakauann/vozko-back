package user_usecase

import (
	"sort"
	"time"

	"vozko/domain/auth"
	"vozko/domain/cache"
	"vozko/domain/user"
	"vozko/domain/workspace"
)

const (
	revokedJTIPrefix = "revoked_jti:"
	revokedJTITTL    = 16 * time.Minute
)

type deleteUserUseCase struct {
	userRepo        user.UserRepository
	wsRepo          workspace.Repository
	sessionRepo     auth.SessionRepository
	passwordService auth.PasswordService
	shared          cache.SharedState
}

var _ user.DeleteUserUseCase = (*deleteUserUseCase)(nil)

func NewDeleteUserUseCase(
	userRepo user.UserRepository,
	wsRepo workspace.Repository,
	sessionRepo auth.SessionRepository,
	passwordService auth.PasswordService,
	shared cache.SharedState,
) user.DeleteUserUseCase {
	return &deleteUserUseCase{
		userRepo:        userRepo,
		wsRepo:          wsRepo,
		sessionRepo:     sessionRepo,
		passwordService: passwordService,
		shared:          shared,
	}
}

type workspaceAction struct {
	workspaceID    string
	myMemberID     string
	isOwner        bool
	orphan         bool
	successorID    string
	successorMemID string
}

func (uc *deleteUserUseCase) Execute(userID, currentPassword string) error {
	u, err := uc.userRepo.FindByID(userID)
	if err != nil {
		return err
	}

	if err := uc.passwordService.Verify(u.Password, currentPassword); err != nil {
		return user.ErrInvalidPassword
	}

	wss, err := uc.wsRepo.ListWorkspacesByUser(userID, "", "")
	if err != nil {
		return err
	}

	actions := make([]workspaceAction, 0, len(wss))
	for _, ws := range wss {
		members, err := uc.wsRepo.ListMembers(ws.ID)
		if err != nil {
			return err
		}

		var myMemberID string
		others := make([]*workspace.Member, 0, len(members))
		for _, m := range members {
			if m.UserID == userID {
				myMemberID = m.ID
				continue
			}
			others = append(others, m)
		}

		act := workspaceAction{
			workspaceID: ws.ID,
			myMemberID:  myMemberID,
			isOwner:     ws.CurrentUserRole == workspace.RoleOwner,
		}

		if act.isOwner {
			if successor := pickSuccessor(others); successor != nil {
				act.successorID = successor.UserID
				act.successorMemID = successor.ID
			} else {
				act.orphan = true
			}
		}

		actions = append(actions, act)
	}

	uc.revokeSessions(userID)

	for _, act := range actions {
		if act.isOwner {
			if err := uc.wsRepo.TransferOwnership(act.workspaceID, act.successorID); err != nil {
				return err
			}
			if !act.orphan && act.successorMemID != "" {
				_ = uc.wsRepo.UpdateMemberRole(act.successorMemID, workspace.RoleOwner)
			}
		}
		if act.myMemberID != "" {
			if err := uc.wsRepo.RemoveMember(act.myMemberID); err != nil {
				return err
			}
		}
	}

	if err := uc.wsRepo.DetachUserAuthoredRefs(userID); err != nil {
		return err
	}

	return uc.userRepo.Delete(userID)
}

func pickSuccessor(others []*workspace.Member) *workspace.Member {
	admins := make([]*workspace.Member, 0, len(others))
	for _, m := range others {
		if m.Role == workspace.RoleAdmin {
			admins = append(admins, m)
		}
	}
	if len(admins) == 0 {
		return nil
	}
	sort.SliceStable(admins, func(i, j int) bool {
		return admins[i].CreatedAt.Before(admins[j].CreatedAt)
	})
	return admins[0]
}

func (uc *deleteUserUseCase) revokeSessions(userID string) {
	sessions, _ := uc.sessionRepo.FindActiveByUserID(userID)
	for _, s := range sessions {
		if s.AccessJTI != "" {
			_ = uc.shared.SetString(revokedJTIPrefix+s.AccessJTI, "1", revokedJTITTL)
		}
	}
	_ = uc.sessionRepo.RevokeAllByUserID(userID)
	_, _ = uc.userRepo.IncrementTokenVersion(userID)
	_ = uc.shared.Del("cache:token_ver:" + userID)
}
