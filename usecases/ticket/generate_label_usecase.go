package ticket_usecase

import (
	"fmt"
	"time"

	"vozko/domain/address"
	"vozko/domain/order"
	"vozko/domain/ticket"
)

type generateLabelUseCase struct {
	ticketRepo  ticket.Repository
	orderRepo   order.OrderRepository
	addressRepo address.AddressRepository
}

func NewGenerateLabelUseCase(ticketRepo ticket.Repository, orderRepo order.OrderRepository, addressRepo address.AddressRepository) ticket.GenerateLabelUseCase {
	return &generateLabelUseCase{
		ticketRepo:  ticketRepo,
		orderRepo:   orderRepo,
		addressRepo: addressRepo,
	}
}

func (uc *generateLabelUseCase) Execute(ticketID, actorID, trackingCode string) (map[string]interface{}, error) {
	t, err := uc.ticketRepo.FindByID(ticketID)
	if err != nil {
		return nil, err
	}

	if trackingCode == "" {
		return nil, ticket.ErrTrackingCodeRequired
	}

	if !ticket.CanTransition(t.Status, ticket.StatusLabelGenerated) && t.Status != ticket.StatusLabelGenerated {
		return nil, ticket.ErrInvalidStatusTransition
	}

	orderData, err := uc.orderRepo.GetByIDForSystem(t.OrderID)
	if err != nil {
		return nil, err
	}

	addr, err := uc.addressRepo.GetByID(orderData.UserID, orderData.ShippingAddressID)
	if err != nil {
		return nil, err
	}

	label := map[string]interface{}{
		"ticketId":     t.ID,
		"orderId":      orderData.ID,
		"userId":       orderData.UserID,
		"generatedAt":  time.Now().Format(time.RFC3339),
		"customerName": orderData.CustomerName,
		"shippingAddress": map[string]interface{}{
			"name":       addr.Name,
			"street":     fmt.Sprintf("%s, %s", addr.Street, addr.Number),
			"complement": addr.Complement,
			"district":   addr.District,
			"city":       addr.City,
			"state":      addr.State,
			"zipCode":    addr.ZipCode,
		},
	}

	t.Status = ticket.StatusLabelGenerated
	t.LastStatusBy = &actorID
	now := time.Now()
	t.LastStatusAt = &now
	tc := trackingCode
	t.TrackingCode = &tc
	if err := uc.ticketRepo.Update(t); err != nil {
		return nil, err
	}

	return label, nil
}
