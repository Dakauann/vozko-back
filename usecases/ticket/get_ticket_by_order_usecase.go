package ticket_usecase

import "vozko/domain/ticket"

type getTicketByOrderUseCase struct {
	ticketRepo ticket.Repository
}

func NewGetTicketByOrderUseCase(ticketRepo ticket.Repository) ticket.GetTicketByOrderUseCase {
	return &getTicketByOrderUseCase{ticketRepo: ticketRepo}
}

func (uc *getTicketByOrderUseCase) Execute(userID, orderID string) (*ticket.Ticket, error) {
	t, err := uc.ticketRepo.FindByOrderID(orderID)
	if err != nil {
		return nil, err
	}

	if t.UserID != userID {
		return nil, ticket.ErrTicketUnauthorized
	}

	return t, nil
}
