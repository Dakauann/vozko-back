package user_usecase

import "vozko/domain/user"

type findUserByIDUseCase struct {
	userRepo user.UserRepository
}

func NewFindUserByIDUseCase(userRepo user.UserRepository) user.FindUserByIDUseCase {
	return &findUserByIDUseCase{
		userRepo: userRepo,
	}
}

func (uc *findUserByIDUseCase) Execute(userId string) (*user.User, error) {
	return uc.userRepo.FindByID(userId)
}
