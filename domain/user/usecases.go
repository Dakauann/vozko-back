package user

import "vozko/domain/shared"

type CreateUserUseCase interface {
	Execute(user *User) error
}

type UpdateUserUseCase interface {
	Execute(userId string, user *User) error
}

type DeleteUserUseCase interface {
	// Execute deletes the authenticated user's own account after verifying their
	// current password. It removes the user's memberships, transfers ownership of
	// any owned workspaces to a remaining member, revokes the user's sessions, and
	// hard-deletes the user record and personal data.
	Execute(userID, currentPassword string) error
}

type FindUserByIDUseCase interface {
	Execute(userId string) (*User, error)
}

type FindUserByEmailUseCase interface {
	Execute(email string) (*User, error)
}

type ListUsersUseCase interface {
	Execute(input ListUsersInput) (*shared.PaginatedResult[*User], error)
}

type UpdateUserRoleUseCase interface {
	Execute(actorUserID, targetUserID string, newRole Role) error
}
