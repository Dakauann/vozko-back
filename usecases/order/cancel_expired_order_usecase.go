package order_usecase

import (
	"fmt"

	"vozko/brand"
	"vozko/domain/notification"
	"vozko/domain/order"
	"vozko/domain/payment"
	"vozko/domain/product"
	"vozko/domain/user"
)

type cancelExpiredOrderUseCase struct {
	orderRepo    order.OrderRepository
	paymentRepo  payment.PaymentRepository
	emailService notification.EmailService
	userRepo     user.UserRepository
	productRepo  product.ProductRepository
}

func NewCancelExpiredOrderUseCase(orderRepo order.OrderRepository, paymentRepo payment.PaymentRepository, emailService notification.EmailService, userRepo user.UserRepository, productRepo product.ProductRepository) order.CancelExpiredOrderUseCase {
	return &cancelExpiredOrderUseCase{
		orderRepo:    orderRepo,
		paymentRepo:  paymentRepo,
		emailService: emailService,
		userRepo:     userRepo,
		productRepo:  productRepo,
	}
}

func (uc *cancelExpiredOrderUseCase) Execute() error {
	expiredOrders, err := uc.orderRepo.GetExpiredOrders()
	if err != nil {
		return err
	}

	var cancellableOrders []*order.Order
	for _, expiredOrder := range expiredOrders {
		if expiredOrder.Payment != nil {
			switch expiredOrder.Payment.Status {
			case payment.StatusReceived, payment.StatusReceivedInCash:
				continue
			}
		}
		cancellableOrders = append(cancellableOrders, expiredOrder)
	}

	if len(cancellableOrders) == 0 {
		return nil
	}

	orderIDs := make([]string, 0, len(cancellableOrders))
	for _, ord := range cancellableOrders {
		orderIDs = append(orderIDs, ord.ID)
	}

	if err := uc.orderRepo.CancelExpiredOrdersAndReturnInventory(orderIDs); err != nil {
		return err
	}

	for _, ord := range cancellableOrders {
		go uc.sendCancellationEmail(ord)
	}

	return nil
}

func (uc *cancelExpiredOrderUseCase) cancelPayment(orderID string) error {
	return uc.paymentRepo.CancelPayment(orderID)
}

func (uc *cancelExpiredOrderUseCase) sendCancellationEmail(order *order.Order) {
	subject := "Pedido Cancelado - " + brand.Active().Name
	if err := uc.emailService.SendTemplate(uc.getUserEmail(order.UserID), subject, "order_cancelled_expired.html", map[string]interface{}{
		"OrderID":      order.ID,
		"CustomerName": order.CustomerName,
		"TotalAmount":  fmt.Sprintf("%.2f", order.TotalAmount),
	}); err != nil {
		return
	}
}

func (uc *cancelExpiredOrderUseCase) getUserEmail(userID string) string {
	u, err := uc.userRepo.FindByID(userID)
	if err != nil {
		return ""
	}
	return u.Email
}

func (uc *cancelExpiredOrderUseCase) loadAndRenderTemplate(filePath string, data map[string]interface{}) (string, error) {
	return "", fmt.Errorf("loadAndRenderTemplate is deprecated; use EmailService.SendTemplate instead")
}
