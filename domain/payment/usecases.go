package payment

type CreatePaymentUseCase interface {
	CreatePayment(userID string, amount int64, currency string) (Payment, error)
}

type GetPaymentStatusUseCase interface {
	GetPaymentStatus(paymentID string) (Status, error)
}

type UpdatePaymentStatusUseCase interface {
	UpdatePaymentStatus(paymentID string, status Status) error
}

type ListUserPaymentsUseCase interface {
	ListUserPayments(userID string) ([]Payment, error)
}

type RefundPaymentUseCase interface {
	RefundPayment(paymentID string, amount int64) error
}

type HandleAsaasWebhookUseCase interface {
	Execute(event *AsaasWebhookEvent) error
}

type ConsumeAsaasWebhookUseCase interface {
	Start() error
}

type CreatePaymentSplitUseCase interface {
	Execute(split *PaymentSplit) (*PaymentSplit, error)
}

type UpdatePaymentSplitUseCase interface {
	Execute(id string, split *PaymentSplit) (*PaymentSplit, error)
}

type DeletePaymentSplitUseCase interface {
	Execute(id string) error
}

type GetPaymentSplitUseCase interface {
	Execute(id string) (*PaymentSplit, error)
}

type GetPaymentSplitSuppliersUseCase interface {
	Execute() ([]*PaymentSplit, error)
}

type ListPaymentSplitsUseCase interface {
	Execute() ([]PaymentSplit, error)
}
