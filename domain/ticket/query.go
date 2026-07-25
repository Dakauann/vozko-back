package ticket

import "vozko/domain/shared"

type ListTicketsInput struct {
	Options shared.QueryOptions
}

type ListUserTicketsInput struct {
	UserID  string
	Options shared.QueryOptions
}
