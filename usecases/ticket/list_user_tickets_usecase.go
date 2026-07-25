package ticket_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/ticket"
)

type listUserTicketsUseCase struct {
	ticketRepo ticket.Repository
}

func NewListUserTicketsUseCase(ticketRepo ticket.Repository) ticket.ListUserTicketsUseCase {
	return &listUserTicketsUseCase{ticketRepo: ticketRepo}
}

func (uc *listUserTicketsUseCase) Execute(input ticket.ListUserTicketsInput) (*shared.PaginatedResult[*ticket.Ticket], error) {
	return uc.ticketRepo.ListByWorkspace(input)
}
