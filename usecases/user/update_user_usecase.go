package user_usecase

import "vozko/domain/user"

type updateUserUseCase struct {
	userRepo user.UserRepository
}

func NewUpdateUserUseCase(userRepo user.UserRepository) user.UpdateUserUseCase {
	return &updateUserUseCase{userRepo: userRepo}
}

func (uc *updateUserUseCase) Execute(userID string, u *user.User) error {
	return uc.userRepo.Update(userID, u)
}
