package ticket_usecase

import (
	"fmt"
	"time"

	"vozko/domain/notification"
	"vozko/domain/ticket"
	"vozko/domain/user"
)

type updateTicketStatusUseCase struct {
	ticketRepo  ticket.Repository
	userRepo    user.UserRepository
	emailSender notification.EmailService
}

func NewUpdateTicketStatusUseCase(ticketRepo ticket.Repository, userRepo user.UserRepository, emailSender notification.EmailService) ticket.UpdateTicketStatusUseCase {
	return &updateTicketStatusUseCase{
		ticketRepo:  ticketRepo,
		userRepo:    userRepo,
		emailSender: emailSender,
	}
}

func (uc *updateTicketStatusUseCase) Execute(ticketID, actorID string, nextStatus ticket.Status, trackingCode *string) (*ticket.Ticket, error) {
	if _, err := ticket.ValidateStatus(string(nextStatus)); err != nil {
		return nil, err
	}

	t, err := uc.ticketRepo.FindByID(ticketID)
	if err != nil {
		return nil, err
	}

	if !ticket.CanTransition(t.Status, nextStatus) {
		return nil, ticket.ErrInvalidStatusTransition
	}

	now := time.Now()
	t.Status = nextStatus
	t.LastStatusBy = &actorID
	t.LastStatusAt = &now

	switch nextStatus {
	case ticket.StatusWaitingDocuments:
		t.LastRevertedBy = &actorID
		t.TrackingCode = nil
	case ticket.StatusLabelGenerated:
		if trackingCode == nil || *trackingCode == "" {
			return nil, ticket.ErrTrackingCodeRequired
		}
		t.TrackingCode = trackingCode
	default:
	}

	if err := uc.ticketRepo.Update(t); err != nil {
		return nil, err
	}

	if nextStatus == ticket.StatusWaitingDocuments {
		uc.notifyTicketReset(t)
	}

	return t, nil
}

func (uc *updateTicketStatusUseCase) notifyTicketReset(t *ticket.Ticket) {
	if uc.emailSender == nil {
		return
	}

	if t == nil || t.UserID == "" {
		return
	}

	u, err := uc.userRepo.FindByID(t.UserID)
	if err != nil || u == nil || u.Email == "" {
		return
	}

	displayName := u.Username
	if displayName == "" {
		displayName = u.Email
	}

	subject := "Documentos adicionais necessários"
	body := fmt.Sprintf("Olá %s,\n\nPrecisamos que reenviem os documentos do pedido %s para continuar o processo de desembaraço.", displayName, t.OrderID)
	uc.emailSender.SendEmail(u.Email, subject, body)
}
