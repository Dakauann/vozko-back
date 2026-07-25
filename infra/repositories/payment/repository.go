package payment_repository

import (
	"errors"

	"vozko/domain/payment"
	"vozko/infra/database/schema"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) payment.PaymentRepository {
	return &repository{db: db}
}

func (r *repository) CreatePayment(payment *payment.Payment) error {
	if payment.ID == "" {
		payment.ID = uuid.New().String()
	}

	dbPayment := &schema.Payment{
		ID:        payment.ID,
		Amount:    payment.Amount,
		Status:    string(payment.Status),
		PixQrCode: getStringValue(payment.PixQrCode),
		PixCopy:   getStringValue(payment.PixCopy),
	}

	if payment.ExternalID != "" {
		dbPayment.ExternalID = payment.ExternalID
	}

	var orderID string
	if payment.OrderID != nil {
		orderID = *payment.OrderID
	}

	if orderID != "" {
		dbPayment.OrderID = orderID
	}

	paymentMethod := "pix"
	if payment.BillingType != "" {
		paymentMethod = string(payment.BillingType)
	}
	dbPayment.PaymentMethod = paymentMethod

	return r.db.Create(dbPayment).Error
}

func (r *repository) GetPaymentByID(paymentID string) (*payment.Payment, error) {
	var dbPayment schema.Payment
	if err := r.db.Where("id = ?", paymentID).First(&dbPayment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, payment.ErrPaymentNotFound
		}
		return nil, err
	}

	return mapToPayment(&dbPayment), nil
}

func (r *repository) GetPaymentByExternalID(externalID string) (*payment.Payment, error) {
	var dbPayment schema.Payment
	if err := r.db.Where("external_id = ?", externalID).First(&dbPayment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, payment.ErrPaymentNotFound
		}
		return nil, err
	}

	return mapToPayment(&dbPayment), nil
}

func (r *repository) UpdatePaymentStatus(paymentID string, status payment.Status) error {
	return r.db.Model(&schema.Payment{}).
		Where("id = ?", paymentID).
		Update("status", string(status)).Error
}

func (r *repository) UpdatePaymentStatusByExternalID(externalID string, status payment.Status) error {
	return r.db.Model(&schema.Payment{}).
		Where("external_id = ?", externalID).
		Update("status", string(status)).Error
}

func (r *repository) ListPaymentsByUser(userID string) ([]payment.Payment, error) {
	var dbPayments []schema.Payment
	if err := r.db.Where("user_id = ?", userID).Find(&dbPayments).Error; err != nil {
		return nil, err
	}

	payments := make([]payment.Payment, len(dbPayments))
	for i, dbPayment := range dbPayments {
		payments[i] = *mapToPayment(&dbPayment)
	}
	return payments, nil
}

func (r *repository) RefundPayment(paymentID string, amount int64) error {
	return r.db.Model(&schema.Payment{}).
		Where("id = ?", paymentID).
		Update("status", string(payment.StatusRefunded)).Error
}

func (r *repository) CancelPayment(orderID string) error {
	return r.db.Model(&schema.Payment{}).
		Where("order_id = ?", orderID).
		Update("status", string(payment.StatusCancelled)).Error
}

func mapToPayment(dbPayment *schema.Payment) *payment.Payment {
	payment := &payment.Payment{
		ID:     dbPayment.ID,
		Amount: dbPayment.Amount,
		Status: payment.Status(dbPayment.Status),
	}

	if dbPayment.ExternalID != "" {
		payment.ExternalID = dbPayment.ExternalID
	}

	if dbPayment.OrderID != "" {
		payment.OrderID = &dbPayment.OrderID
	}

	if dbPayment.PixQrCode != "" {
		payment.PixQrCode = &dbPayment.PixQrCode
	}

	if dbPayment.PixCopy != "" {
		payment.PixCopy = &dbPayment.PixCopy
	}

	return payment
}

func getStringValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
