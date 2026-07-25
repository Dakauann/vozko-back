package user_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/user"
)

type listUsersUseCase struct {
	userRepo user.UserRepository
}

func NewListUsersUseCase(userRepo user.UserRepository) user.ListUsersUseCase {
	return &listUsersUseCase{
		userRepo: userRepo,
	}
}

func (uc *listUsersUseCase) Execute(input user.ListUsersInput) (*shared.PaginatedResult[*user.User], error) {
	return uc.userRepo.List(input)
}
