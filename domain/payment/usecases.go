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

// HandlePaymentWebhookUseCase applies a canonical payment notification to local state
// (payments, orders, invoices, balance, subscriptions). It is provider-agnostic: each
// provider's consumer translates into WebhookEvent before calling it.
type HandlePaymentWebhookUseCase interface {
	Execute(event *WebhookEvent) error
}

// ConsumePaymentWebhookUseCase starts a provider's webhook queue consumer.
type ConsumePaymentWebhookUseCase interface {
	Start() error
}

// HandleAsaasWebhookUseCase is the pre-multi-provider name.
//
// Deprecated: use HandlePaymentWebhookUseCase.
type HandleAsaasWebhookUseCase = HandlePaymentWebhookUseCase

// ConsumeAsaasWebhookUseCase is the pre-multi-provider name.
//
// Deprecated: use ConsumePaymentWebhookUseCase.
type ConsumeAsaasWebhookUseCase = ConsumePaymentWebhookUseCase

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
