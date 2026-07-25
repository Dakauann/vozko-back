package user

import "vozko/domain/shared"

type ListUsersInput struct {
	Search string
	Role   *Role
	shared.QueryOptions
}

type UserRepository interface {
	WithTx(tx interface{}) UserRepository
	Create(user *User) error
	Update(userId string, user *User) error
	Delete(userId string) error
	FindByID(userId string) (*User, error)
	FindByIDs(userIds []string) ([]*User, error)
	FindByEmail(email string) (*User, error)
	FindByDocument(doc string) (*User, error)
	List(input ListUsersInput) (*shared.PaginatedResult[*User], error)
	CountByRole(role Role) (int64, error)
	GetUserRole(userId string) (string, error)
	GetTokenVersion(userId string) (int, error)
	IncrementTokenVersion(userId string) (int, error)
}
