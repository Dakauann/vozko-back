package ticket

import "vozko/domain/shared"

type CreateTicketUseCase interface {
	Execute(orderID, userID string) (*Ticket, error)
}

type GetTicketByOrderUseCase interface {
	Execute(userID, orderID string) (*Ticket, error)
}

type ListTicketsUseCase interface {
	Execute(input ListTicketsInput) (*shared.PaginatedResult[*Ticket], error)
}

type ListUserTicketsUseCase interface {
	Execute(input ListUserTicketsInput) (*shared.PaginatedResult[*Ticket], error)
}

type UploadTicketDocumentUseCase interface {
	Execute(userID, ticketID, documentType, fileName, contentType string, content []byte) (*Document, error)
}

type UpdateTicketStatusUseCase interface {
	Execute(ticketID, actorID string, nextStatus Status, trackingCode *string) (*Ticket, error)
}

type GenerateLabelUseCase interface {
	Execute(ticketID, actorID, trackingCode string) (map[string]interface{}, error)
}
