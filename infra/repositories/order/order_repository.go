package order_repository

import (
	"strconv"
	"strings"
	"time"

	"vozko/domain/inventory"
	"vozko/domain/order"
	"vozko/domain/payment"
	"vozko/domain/shared"
	"vozko/domain/ticket"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

func encryptOrderDocument(plain string) (piigorm.EncryptedString, piigorm.BlindIndex, error) {
	if plain == "" {
		return piigorm.Null(), nil, nil
	}
	bi, err := piigorm.NewBlindIndex(schema.OrderCustomerDocumentBlindScope, plain)
	if err != nil {
		return piigorm.EncryptedString{}, nil, err
	}
	return piigorm.NewEncrypted(plain), bi, nil
}

type repository struct {
	db           *gorm.DB
	stockService inventory.VariantStockService
}

func NewRepository(db *gorm.DB, stockService inventory.VariantStockService) order.OrderRepository {
	return &repository{db: db, stockService: stockService}
}

func (r *repository) Create(ord *order.Order) error {
	docEnc, docBlind, err := encryptOrderDocument(ord.CustomerDocument)
	if err != nil {
		return err
	}
	dbOrder := &schema.Order{
		ID:                    ord.ID,
		UserID:                ord.UserID,
		ShippingAddressID:     ord.ShippingAddressID,
		CustomerName:          ord.CustomerName,
		CustomerDocument:      docEnc,
		CustomerDocumentBlind: docBlind,
		TotalAmount:           ord.TotalAmount,
		Status:                string(ord.Status),
		ExpiresAt:             ord.ExpiresAt,
	}

	err = r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(dbOrder).Error; err != nil {
			return err
		}

		ord.ID = dbOrder.ID

		for _, item := range ord.Items {
			dbItem := &schema.OrderItem{
				OrderID:    dbOrder.ID,
				ProductID:  item.ProductID,
				VariantID:  item.VariantID,
				SKU:        item.SKU,
				Quantity:   item.Quantity,
				UnitPrice:  item.UnitPrice,
				TotalPrice: item.TotalPrice,
			}
			if err := tx.Create(dbItem).Error; err != nil {
				return err
			}

			for _, option := range item.Options {
				dbOption := &schema.OrderItemOption{
					OrderItemID: dbItem.ID,
					OptionType:  option.OptionType,
					OptionValue: option.OptionValue,
				}
				if err := tx.Create(dbOption).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})

	return err
}

func (r *repository) CreateWithInventoryReservation(ord *order.Order, inventoryUpdates []order.InventoryUpdate) error {
	docEnc, docBlind, err := encryptOrderDocument(ord.CustomerDocument)
	if err != nil {
		return err
	}
	dbOrder := &schema.Order{
		ID:                    ord.ID,
		UserID:                ord.UserID,
		ShippingAddressID:     ord.ShippingAddressID,
		CustomerName:          ord.CustomerName,
		CustomerDocument:      docEnc,
		CustomerDocumentBlind: docBlind,
		TotalAmount:           ord.TotalAmount,
		Status:                string(ord.Status),
		ExpiresAt:             ord.ExpiresAt,
	}

	err = r.db.Transaction(func(tx *gorm.DB) error {
		for _, update := range inventoryUpdates {
			var variant schema.Variant
			if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", update.VariantID).First(&variant).Error; err != nil {
				return err
			}

			components, err := r.stockService.GetSnapshot(update.VariantID)
			if err != nil {
				return err
			}

			available := variant.Inventory + components.Launched - components.Sold - components.Reserved
			if available < 0 {
				available = 0
			}

			required := update.Quantity
			if required < 0 {
				required = -required
			}

			if available < required {
				return order.ErrInsufficientStock
			}
		}

		if err := tx.Create(dbOrder).Error; err != nil {
			return err
		}

		ord.ID = dbOrder.ID

		for _, item := range ord.Items {
			dbItem := &schema.OrderItem{
				OrderID:    dbOrder.ID,
				ProductID:  item.ProductID,
				VariantID:  item.VariantID,
				SKU:        item.SKU,
				Quantity:   item.Quantity,
				UnitPrice:  item.UnitPrice,
				TotalPrice: item.TotalPrice,
			}
			if err := tx.Create(dbItem).Error; err != nil {
				return err
			}

			for _, option := range item.Options {
				dbOption := &schema.OrderItemOption{
					OrderItemID: dbItem.ID,
					OptionType:  option.OptionType,
					OptionValue: option.OptionValue,
				}
				if err := tx.Create(dbOption).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})

	return err
}

func (r *repository) Update(ord *order.Order) error {
	dbOrder := &schema.Order{
		Status: string(ord.Status),
	}
	return r.db.Model(&schema.Order{}).
		Where("id = ? AND user_id = ?", ord.ID, ord.UserID).
		Updates(dbOrder).Error
}

func (r *repository) UpdateStatus(orderID string, status order.Status) error {
	return r.db.Model(&schema.Order{}).
		Where("id = ?", orderID).
		Update("status", string(status)).Error
}

func (r *repository) GetByID(userID string, orderID string) (*order.Order, error) {
	var dbOrder schema.Order
	if err := r.db.Preload("Items.Options").Preload("Payment").Preload("Ticket.Documents").
		Where("id = ? AND user_id = ?", orderID, userID).
		First(&dbOrder).Error; err != nil {
		return nil, err
	}

	return r.mapToOrder(&dbOrder), nil
}

func (r *repository) ListByWorkspace(input order.ListOrdersInput) (*shared.PaginatedResult[*order.Order], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	countQuery := r.db.Model(&schema.Order{}).
		Where("orders.user_id = ?", input.UserID).
		Select("orders.id").
		Distinct()
	countQuery = r.applyOrderFilters(countQuery, input.Options.Filters)

	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, err
	}

	dataQuery := r.db.Model(&schema.Order{}).
		Select("DISTINCT ON (orders.id) orders.*").
		Preload("Items.Options").
		Preload("Payment").
		Preload("Ticket.Documents").
		Where("orders.user_id = ?", input.UserID)

	dataQuery = r.applyOrderFilters(dataQuery, input.Options.Filters)
	dataQuery = dataQuery.Order("orders.id")
	dataQuery = r.applyOrderSorts(dataQuery, input.Options.Sorts)

	var dbOrders []schema.Order
	if err := dataQuery.
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&dbOrders).Error; err != nil {
		return nil, err
	}

	orders := make([]*order.Order, len(dbOrders))
	for i := range dbOrders {
		dbOrder := dbOrders[i]
		orders[i] = r.mapToOrder(&dbOrder)
	}

	return shared.NewPaginatedResult(orders, pagination, total), nil
}

func (r *repository) mapToOrder(dbOrder *schema.Order) *order.Order {
	items := make([]order.OrderItem, len(dbOrder.Items))
	for i, dbItem := range dbOrder.Items {
		options := make([]order.OrderItemOption, len(dbItem.Options))
		for j, dbOption := range dbItem.Options {
			options[j] = order.OrderItemOption{
				OptionType:  dbOption.OptionType,
				OptionValue: dbOption.OptionValue,
			}
		}

		items[i] = order.OrderItem{
			ID:         dbItem.ID,
			OrderID:    dbItem.OrderID,
			ProductID:  dbItem.ProductID,
			VariantID:  dbItem.VariantID,
			SKU:        dbItem.SKU,
			Quantity:   dbItem.Quantity,
			UnitPrice:  dbItem.UnitPrice,
			TotalPrice: dbItem.TotalPrice,
			Options:    options,
		}
	}

	var paymentData *payment.Payment
	if dbOrder.Payment != nil {
		paymentData = &payment.Payment{
			ID:        dbOrder.Payment.ID,
			Amount:    dbOrder.Payment.Amount,
			Status:    payment.Status(dbOrder.Payment.Status),
			PixQrCode: &dbOrder.Payment.PixQrCode,
			PixCopy:   &dbOrder.Payment.PixCopy,
		}
	}

	ticketData := mapTicketToDomain(dbOrder.Ticket)

	return &order.Order{
		ID:                dbOrder.ID,
		UserID:            dbOrder.UserID,
		ShippingAddressID: dbOrder.ShippingAddressID,
		CustomerName:      dbOrder.CustomerName,
		CustomerDocument:  dbOrder.CustomerDocument.String(),
		Items:             items,
		Payment:           paymentData,
		Ticket:            ticketData,
		TotalAmount:       dbOrder.TotalAmount,
		Status:            order.Status(dbOrder.Status),
		ExpiresAt:         dbOrder.ExpiresAt,
		CreatedAt:         dbOrder.CreatedAt,
		UpdatedAt:         dbOrder.UpdatedAt,
	}
}

func (r *repository) GetByIDForSystem(orderID string) (*order.Order, error) {
	var dbOrder schema.Order
	if err := r.db.Preload("Items.Options").Preload("Payment").Preload("Ticket.Documents").
		Where("id = ?", orderID).
		First(&dbOrder).Error; err != nil {
		return nil, err
	}

	return r.mapToOrder(&dbOrder), nil
}

func (r *repository) GetExpiredOrders() ([]*order.Order, error) {
	var dbOrders []schema.Order
	now := time.Now()

	err := r.db.Preload("Items.Options").Preload("Payment").Preload("Ticket.Documents").
		Where("status = ? AND expires_at IS NOT NULL AND expires_at < ?", "pending", now).
		Find(&dbOrders).Error
	if err != nil {
		return nil, err
	}

	orders := make([]*order.Order, len(dbOrders))
	for i, dbOrder := range dbOrders {
		orders[i] = r.mapToOrder(&dbOrder)
	}
	return orders, nil
}

func mapTicketToDomain(dbTicket *schema.Ticket) *ticket.Ticket {
	if dbTicket == nil {
		return nil
	}

	docs := make([]ticket.Document, len(dbTicket.Documents))
	for i := range dbTicket.Documents {
		doc := dbTicket.Documents[i]
		docs[i] = ticket.Document{
			ID:          doc.ID,
			TicketID:    doc.TicketID,
			Type:        ticket.DocumentType(doc.Type),
			FileName:    doc.FileName,
			ContentType: doc.ContentType,
			URL:         doc.URL,
			UploadedBy:  doc.UploadedBy,
			CreatedAt:   doc.CreatedAt,
		}
	}

	return &ticket.Ticket{
		ID:             dbTicket.ID,
		OrderID:        dbTicket.OrderID,
		UserID:         dbTicket.UserID,
		Status:         ticket.Status(dbTicket.Status),
		TrackingCode:   dbTicket.TrackingCode,
		ShippingCost:   nil,
		CreatedAt:      dbTicket.CreatedAt,
		UpdatedAt:      dbTicket.UpdatedAt,
		Documents:      docs,
		LastStatusBy:   dbTicket.LastStatusBy,
		LastStatusAt:   dbTicket.LastStatusAt,
		LastRevertedBy: dbTicket.LastRevertedBy,
	}
}

func (r *repository) applyOrderFilters(db *gorm.DB, filters []shared.Filter) *gorm.DB {
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
				query = query.Where("orders.status IN ?", filter.Values)
			} else {
				query = query.Where("orders.status = ?", value)
			}
		case "search":
			pattern := "%" + strings.ToLower(value) + "%"

			if docBlind, biErr := piigorm.NewBlindIndex(schema.OrderCustomerDocumentBlindScope, value); biErr == nil {
				query = query.Where("(LOWER(orders.customer_name) LIKE ? OR LOWER(orders.id::text) LIKE ? OR orders.customer_document_blind = ?)", pattern, pattern, []byte(docBlind))
			} else {
				query = query.Where("(LOWER(orders.customer_name) LIKE ? OR LOWER(orders.id::text) LIKE ?)", pattern, pattern)
			}
		case "customerdocument":
			docBlind, biErr := piigorm.NewBlindIndex(schema.OrderCustomerDocumentBlindScope, value)
			if biErr != nil {
				continue
			}
			query = query.Where("orders.customer_document_blind = ?", []byte(docBlind))
		case "customername":
			query = query.Where("LOWER(orders.customer_name) LIKE ?", "%"+strings.ToLower(value)+"%")
		case "createdat":
			timestamp, err := time.Parse(time.RFC3339, value)
			if err != nil {
				continue
			}
			switch filter.Operator {
			case shared.FilterOpGte:
				query = query.Where("orders.created_at >= ?", timestamp)
			case shared.FilterOpLte:
				query = query.Where("orders.created_at <= ?", timestamp)
			default:
				query = query.Where("orders.created_at = ?", timestamp)
			}
		case "updatedat":
			timestamp, err := time.Parse(time.RFC3339, value)
			if err != nil {
				continue
			}
			switch filter.Operator {
			case shared.FilterOpGte:
				query = query.Where("orders.updated_at >= ?", timestamp)
			case shared.FilterOpLte:
				query = query.Where("orders.updated_at <= ?", timestamp)
			default:
				query = query.Where("orders.updated_at = ?", timestamp)
			}
		case "expiresat":
			timestamp, err := time.Parse(time.RFC3339, value)
			if err != nil {
				continue
			}
			switch filter.Operator {
			case shared.FilterOpGte:
				query = query.Where("orders.expires_at >= ?", timestamp)
			case shared.FilterOpLte:
				query = query.Where("orders.expires_at <= ?", timestamp)
			default:
				query = query.Where("orders.expires_at = ?", timestamp)
			}
		case "totalmin":
			amount, err := strconv.ParseFloat(value, 64)
			if err != nil {
				continue
			}
			query = query.Where("orders.total_amount >= ?", amount)
		case "totalmax":
			amount, err := strconv.ParseFloat(value, 64)
			if err != nil {
				continue
			}
			query = query.Where("orders.total_amount <= ?", amount)
		case "paymentstatus":
			query = query.Joins("LEFT JOIN payments ON payments.order_id = orders.id").
				Where("payments.status = ?", value)
		case "addressid", "shippingaddressid":
			query = query.Where("orders.shipping_address_id = ?", value)
		}
	}

	return query
}

func (r *repository) applyOrderSorts(db *gorm.DB, sorts []shared.Sort) *gorm.DB {
	query := db
	if len(sorts) == 0 {
		return query.Order("orders.created_at DESC")
	}

	for _, sort := range sorts {
		direction := "ASC"
		if strings.ToLower(string(sort.Direction)) == string(shared.SortDesc) {
			direction = "DESC"
		}

		switch strings.ToLower(sort.Field) {
		case "createdat":
			query = query.Order("orders.created_at " + direction)
		case "updatedat":
			query = query.Order("orders.updated_at " + direction)
		case "expiresat":
			query = query.Order("orders.expires_at " + direction)
		case "total_amount", "totalamount":
			query = query.Order("orders.total_amount " + direction)
		case "status":
			query = query.Order("orders.status " + direction)
		default:
			continue
		}
	}

	return query
}

func (r *repository) CancelOrder(orderID string) error {
	err := r.db.Model(&schema.Order{}).
		Where("id = ?", orderID).
		Update("status", "cancelled").Error
	return err
}

func (r *repository) CancelExpiredOrdersAndReturnInventory(orderIDs []string) error {
	if len(orderIDs) == 0 {
		return nil
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		var dbOrders []schema.Order
		if err := tx.Where("id IN ?", orderIDs).
			Preload("Items").
			Preload("Payment").
			Find(&dbOrders).Error; err != nil {
			return err
		}

		cancelOrderIDs := make([]string, 0, len(dbOrders))
		cancelOrdersWithPayment := make([]string, 0, len(dbOrders))
		for _, dbOrder := range dbOrders {
			if dbOrder.Payment != nil {
				status := payment.Status(dbOrder.Payment.Status)
				if status == payment.StatusReceived || status == payment.StatusReceivedInCash {
					continue
				}
				cancelOrdersWithPayment = append(cancelOrdersWithPayment, dbOrder.ID)
			}
			cancelOrderIDs = append(cancelOrderIDs, dbOrder.ID)
		}

		if len(cancelOrderIDs) > 0 {
			if err := tx.Model(&schema.Order{}).
				Where("id IN ?", cancelOrderIDs).
				Update("status", order.StatusCancelled).Error; err != nil {
				return err
			}
		}

		if len(cancelOrdersWithPayment) > 0 {
			if err := tx.Model(&schema.Payment{}).
				Where("order_id IN ?", cancelOrdersWithPayment).
				Update("status", string(payment.StatusCancelled)).Error; err != nil {
				return err
			}
		}

		// TODO: integrate with inventory service to return reserved stock.

		return nil
	})
}
