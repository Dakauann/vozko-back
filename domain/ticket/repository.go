package ticket

import "vozko/domain/shared"

type Repository interface {
	Create(ticket *Ticket) error
	Update(ticket *Ticket) error
	FindByID(id string) (*Ticket, error)
	FindByOrderID(orderID string) (*Ticket, error)
	ListByWorkspace(input ListUserTicketsInput) (*shared.PaginatedResult[*Ticket], error)
	ListAll(input ListTicketsInput) (*shared.PaginatedResult[*Ticket], error)

	AddDocument(doc *Document) error
	ListDocuments(ticketID string) ([]Document, error)
	DeleteDocument(documentID string) error
}
