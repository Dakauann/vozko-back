package order

import "vozko/domain/shared"

type ListOrdersInput struct {
	UserID  string
	Options shared.QueryOptions
}
