package user_usecase

import "vozko/domain/user"

type updateUserRoleUseCase struct {
	userRepo user.UserRepository
}

func NewUpdateUserRoleUseCase(userRepo user.UserRepository) user.UpdateUserRoleUseCase {
	return &updateUserRoleUseCase{
		userRepo: userRepo,
	}
}

func (uc *updateUserRoleUseCase) Execute(actorUserID, targetUserID string, newRole user.Role) error {
	if actorUserID == targetUserID {
		return user.ErrCannotModifySelf
	}

	if newRole != user.RoleAdmin && newRole != user.RoleUser {
		return user.ErrInvalidRole
	}

	targetUser, err := uc.userRepo.FindByID(targetUserID)
	if err != nil {
		return err
	}

	if targetUser.Role == user.RoleAdmin && newRole == user.RoleUser {
		adminCount, err := uc.userRepo.CountByRole(user.RoleAdmin)
		if err != nil {
			return err
		}
		if adminCount <= 1 {
			return user.ErrLastAdmin
		}
	}

	targetUser.Role = newRole
	return uc.userRepo.Update(targetUserID, targetUser)
}
