package payment

type PaymentRepository interface {
	CreatePayment(payment *Payment) error
	GetPaymentByID(paymentID string) (*Payment, error)
	UpdatePaymentStatus(paymentID string, status Status) error
	ListPaymentsByUser(userID string) ([]Payment, error)
	RefundPayment(paymentID string, amount int64) error
	CancelPayment(orderID string) error
}

type PaymentSplitRepository interface {
	Create(split *PaymentSplit) error
	Update(split *PaymentSplit) error
	Delete(id string) error
	GetByID(id string) (*PaymentSplit, error)
	GetByIDs(ids []string) (map[string]*PaymentSplit, error)
	GetByType(splitType SplitType) ([]*PaymentSplit, error)
	GetSuppliers() ([]*PaymentSplit, error)
	List() ([]PaymentSplit, error)
}
