package ticket_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/ticket"
)

type listTicketsUseCase struct {
	ticketRepo ticket.Repository
}

func NewListTicketsUseCase(ticketRepo ticket.Repository) ticket.ListTicketsUseCase {
	return &listTicketsUseCase{ticketRepo: ticketRepo}
}

func (uc *listTicketsUseCase) Execute(input ticket.ListTicketsInput) (*shared.PaginatedResult[*ticket.Ticket], error) {
	return uc.ticketRepo.ListAll(input)
}
