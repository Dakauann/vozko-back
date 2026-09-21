package user

import "errors"

var (
	ErrNotFound         = errors.New("user not found")
	ErrInvalidRole      = errors.New("invalid role")
	ErrCannotModifySelf = errors.New("cannot modify your own role")
	ErrLastAdmin        = errors.New("cannot remove role from the last admin")
	ErrInvalidPassword  = errors.New("invalid password")
)
