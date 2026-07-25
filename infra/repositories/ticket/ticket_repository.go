package ticket_repository

import (
	"errors"
	"strings"
	"time"

	"vozko/domain/shared"
	"vozko/domain/ticket"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) ticket.Repository {
	return &repository{db: db}
}

func (r *repository) Create(t *ticket.Ticket) error {
	dbTicket := &schema.Ticket{
		ID:             t.ID,
		OrderID:        t.OrderID,
		UserID:         t.UserID,
		Status:         string(t.Status),
		TrackingCode:   t.TrackingCode,
		LastStatusBy:   t.LastStatusBy,
		LastStatusAt:   t.LastStatusAt,
		LastRevertedBy: t.LastRevertedBy,
	}

	if err := r.db.Create(dbTicket).Error; err != nil {
		if isDuplicateEntryError(err) {
			return ticket.ErrTicketAlreadyExists
		}
		return err
	}

	t.ID = dbTicket.ID
	return nil
}

func (r *repository) Update(t *ticket.Ticket) error {
	return r.db.Model(&schema.Ticket{}).
		Where("id = ?", t.ID).
		Updates(map[string]interface{}{
			"status":           string(t.Status),
			"tracking_code":    t.TrackingCode,
			"last_status_by":   t.LastStatusBy,
			"last_status_at":   t.LastStatusAt,
			"last_reverted_by": t.LastRevertedBy,
		}).Error
}

func (r *repository) FindByID(id string) (*ticket.Ticket, error) {
	var dbTicket schema.Ticket
	if err := r.db.Preload("Documents").Where("id = ?", id).First(&dbTicket).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ticket.ErrTicketNotFound
		}
		return nil, err
	}
	return mapToDomain(&dbTicket), nil
}

func (r *repository) FindByOrderID(orderID string) (*ticket.Ticket, error) {
	var dbTicket schema.Ticket
	if err := r.db.Preload("Documents").Where("order_id = ?", orderID).First(&dbTicket).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ticket.ErrTicketNotFound
		}
		return nil, err
	}
	return mapToDomain(&dbTicket), nil
}

func (r *repository) ListByWorkspace(input ticket.ListUserTicketsInput) (*shared.PaginatedResult[*ticket.Ticket], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	countQuery := r.db.Model(&schema.Ticket{}).
		Where("tickets.user_id = ?", input.UserID).
		Select("tickets.id").
		Distinct()
	countQuery = r.applyTicketFilters(countQuery, input.Options.Filters)

	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, err
	}

	dataQuery := r.db.Model(&schema.Ticket{}).
		Select("DISTINCT ON (tickets.id) tickets.*").
		Preload("Documents").
		Where("tickets.user_id = ?", input.UserID)

	dataQuery = r.applyTicketFilters(dataQuery, input.Options.Filters)
	dataQuery = dataQuery.Order("tickets.id")
	dataQuery = r.applyTicketSorts(dataQuery, input.Options.Sorts)

	var dbTickets []schema.Ticket
	if err := dataQuery.
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&dbTickets).Error; err != nil {
		return nil, err
	}

	tickets := make([]*ticket.Ticket, len(dbTickets))
	for i := range dbTickets {
		tickets[i] = mapToDomain(&dbTickets[i])
	}

	return shared.NewPaginatedResult(tickets, pagination, total), nil
}

func (r *repository) ListAll(input ticket.ListTicketsInput) (*shared.PaginatedResult[*ticket.Ticket], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	countQuery := r.db.Model(&schema.Ticket{}).
		Select("tickets.id").
		Distinct()
	countQuery = r.applyTicketFilters(countQuery, input.Options.Filters)

	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, err
	}

	dataQuery := r.db.Model(&schema.Ticket{}).
		Select("DISTINCT ON (tickets.id) tickets.*").
		Preload("Documents")

	dataQuery = r.applyTicketFilters(dataQuery, input.Options.Filters)
	dataQuery = dataQuery.Order("tickets.id")
	dataQuery = r.applyTicketSorts(dataQuery, input.Options.Sorts)

	var dbTickets []schema.Ticket
	if err := dataQuery.
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&dbTickets).Error; err != nil {
		return nil, err
	}

	tickets := make([]*ticket.Ticket, len(dbTickets))
	for i := range dbTickets {
		tickets[i] = mapToDomain(&dbTickets[i])
	}

	return shared.NewPaginatedResult(tickets, pagination, total), nil
}

func (r *repository) AddDocument(doc *ticket.Document) error {
	dbDoc := &schema.TicketDocument{
		ID:          doc.ID,
		TicketID:    doc.TicketID,
		Type:        string(doc.Type),
		FileName:    doc.FileName,
		ContentType: doc.ContentType,
		URL:         doc.URL,
		UploadedBy:  doc.UploadedBy,
	}
	if err := r.db.Create(dbDoc).Error; err != nil {
		return err
	}
	doc.ID = dbDoc.ID
	return nil
}

func (r *repository) ListDocuments(ticketID string) ([]ticket.Document, error) {
	var dbDocs []schema.TicketDocument
	if err := r.db.Where("ticket_id = ?", ticketID).Order("created_at ASC").Find(&dbDocs).Error; err != nil {
		return nil, err
	}

	docs := make([]ticket.Document, len(dbDocs))
	for i := range dbDocs {
		docs[i] = ticket.Document{
			ID:          dbDocs[i].ID,
			TicketID:    dbDocs[i].TicketID,
			Type:        ticket.DocumentType(dbDocs[i].Type),
			FileName:    dbDocs[i].FileName,
			ContentType: dbDocs[i].ContentType,
			URL:         dbDocs[i].URL,
			UploadedBy:  dbDocs[i].UploadedBy,
			CreatedAt:   dbDocs[i].CreatedAt,
		}
	}
	return docs, nil
}

func (r *repository) DeleteDocument(documentID string) error {
	return r.db.Delete(&schema.TicketDocument{}, "id = ?", documentID).Error
}

func (r *repository) applyTicketFilters(db *gorm.DB, filters []shared.Filter) *gorm.DB {
	query := db
	for _, filter := range filters {
		if len(filter.Values) == 0 {
			continue
		}

		value := strings.TrimSpace(filter.Values[0])
		if value == "" {
			continue
		}

		switch strings.ToLower(filter.Field) {
		case "status":
			if filter.Operator == shared.FilterOpIn && len(filter.Values) > 0 {
				query = query.Where("tickets.status IN ?", filter.Values)
			} else {
				query = query.Where("tickets.status = ?", value)
			}
		case "orderid":
			query = query.Where("tickets.order_id = ?", value)
		case "userid":
			query = query.Where("tickets.user_id = ?", value)
		case "trackingcode":
			if filter.Operator == shared.FilterOpLike {
				query = query.Where("LOWER(COALESCE(tickets.tracking_code, '')) LIKE ?", "%"+strings.ToLower(value)+"%")
			} else {
				query = query.Where("tickets.tracking_code = ?", value)
			}
		case "search":
			pattern := "%" + strings.ToLower(value) + "%"
			query = query.Where("(LOWER(tickets.id::text) LIKE ? OR LOWER(tickets.order_id::text) LIKE ? OR LOWER(COALESCE(tickets.tracking_code, '')) LIKE ?)", pattern, pattern, pattern)
		case "createdat":
			timestamp, err := time.Parse(time.RFC3339, value)
			if err != nil {
				continue
			}
			switch filter.Operator {
			case shared.FilterOpGte:
				query = query.Where("tickets.created_at >= ?", timestamp)
			case shared.FilterOpLte:
				query = query.Where("tickets.created_at <= ?", timestamp)
			default:
				query = query.Where("tickets.created_at = ?", timestamp)
			}
		case "updatedat":
			timestamp, err := time.Parse(time.RFC3339, value)
			if err != nil {
				continue
			}
			switch filter.Operator {
			case shared.FilterOpGte:
				query = query.Where("tickets.updated_at >= ?", timestamp)
			case shared.FilterOpLte:
				query = query.Where("tickets.updated_at <= ?", timestamp)
			default:
				query = query.Where("tickets.updated_at = ?", timestamp)
			}
		case "laststatusat":
			timestamp, err := time.Parse(time.RFC3339, value)
			if err != nil {
				continue
			}
			switch filter.Operator {
			case shared.FilterOpGte:
				query = query.Where("tickets.last_status_at >= ?", timestamp)
			case shared.FilterOpLte:
				query = query.Where("tickets.last_status_at <= ?", timestamp)
			default:
				query = query.Where("tickets.last_status_at = ?", timestamp)
			}
		}
	}

	return query
}

func (r *repository) applyTicketSorts(db *gorm.DB, sorts []shared.Sort) *gorm.DB {
	query := db
	if len(sorts) == 0 {
		return query.Order("tickets.created_at DESC")
	}

	for _, sort := range sorts {
		direction := "ASC"
		if strings.ToLower(string(sort.Direction)) == string(shared.SortDesc) {
			direction = "DESC"
		}

		switch strings.ToLower(sort.Field) {
		case "createdat":
			query = query.Order("tickets.created_at " + direction)
		case "updatedat":
			query = query.Order("tickets.updated_at " + direction)
		case "status":
			query = query.Order("tickets.status " + direction)
		case "laststatusat":
			query = query.Order("tickets.last_status_at " + direction)
		default:
			continue
		}
	}

	return query
}

func isDuplicateEntryError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "tickets_order_id_key")
}

func mapToDomain(dbTicket *schema.Ticket) *ticket.Ticket {
	if dbTicket == nil {
		return nil
	}

	docs := make([]ticket.Document, len(dbTicket.Documents))
	for i := range dbTicket.Documents {
		docs[i] = ticket.Document{
			ID:          dbTicket.Documents[i].ID,
			TicketID:    dbTicket.Documents[i].TicketID,
			Type:        ticket.DocumentType(dbTicket.Documents[i].Type),
			FileName:    dbTicket.Documents[i].FileName,
			ContentType: dbTicket.Documents[i].ContentType,
			URL:         dbTicket.Documents[i].URL,
			UploadedBy:  dbTicket.Documents[i].UploadedBy,
			CreatedAt:   dbTicket.Documents[i].CreatedAt,
		}
	}

	return &ticket.Ticket{
		ID:             dbTicket.ID,
		OrderID:        dbTicket.OrderID,
		UserID:         dbTicket.UserID,
		Status:         ticket.Status(dbTicket.Status),
		TrackingCode:   dbTicket.TrackingCode,
		CreatedAt:      dbTicket.CreatedAt,
		UpdatedAt:      dbTicket.UpdatedAt,
		Documents:      docs,
		LastStatusBy:   dbTicket.LastStatusBy,
		LastStatusAt:   dbTicket.LastStatusAt,
		LastRevertedBy: dbTicket.LastRevertedBy,
	}
}
