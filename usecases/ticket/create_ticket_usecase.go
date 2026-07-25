package ticket_usecase

import (
	"time"

	"github.com/google/uuid"

	"vozko/domain/ticket"
)

type createTicketUseCase struct {
	ticketRepo ticket.Repository
}

func NewCreateTicketUseCase(ticketRepo ticket.Repository) ticket.CreateTicketUseCase {
	return &createTicketUseCase{ticketRepo: ticketRepo}
}

func (uc *createTicketUseCase) Execute(orderID, userID string) (*ticket.Ticket, error) {
	if existing, err := uc.ticketRepo.FindByOrderID(orderID); err == nil && existing != nil {
		return existing, ticket.ErrTicketAlreadyExists
	} else if err != nil && err != ticket.ErrTicketNotFound {
		return nil, err
	}

	now := time.Now()
	t := &ticket.Ticket{
		ID:        uuid.New().String(),
		OrderID:   orderID,
		UserID:    userID,
		Status:    ticket.StatusWaitingDocuments,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := uc.ticketRepo.Create(t); err != nil {
		return nil, err
	}

	return t, nil
}
