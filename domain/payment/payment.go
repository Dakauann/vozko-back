package payment

type Status string
type BillingType string

const (
	StatusPending          Status = "PENDING"
	StatusReceived         Status = "RECEIVED"
	StatusOverdue          Status = "OVERDUE"
	StatusRefunded         Status = "REFUNDED"
	StatusReceivedInCash   Status = "RECEIVED_IN_CASH"
	StatusRefundRequested  Status = "REFUND_REQUESTED"
	StatusRefundInProgress Status = "REFUND_IN_PROGRESS"
	StatusCancelled        Status = "CANCELLED"
)

const (
	BillingTypeBoleto BillingType = "boleto"
	BillingTypePix    BillingType = "pix"
)

type Payment struct {
	ID           string      `json:"id"`
	UserID       string      `json:"userId"`
	OrderID      *string     `json:"orderId,omitempty"`
	Amount       float64     `json:"amount"`
	Status       Status      `json:"status"`
	ExternalID   string      `json:"externalId,omitempty"`
	BillingType  BillingType `json:"billingType"`
	CrearedAt    int64       `json:"createdAt"`
	UpdatedAt    int64       `json:"updatedAt"`
	DueDate      int64       `json:"dueDate"`
	PaidDate     int64       `json:"paidDate"`
	Installments int         `json:"installments"`
	Split        []Split     `json:"split"`
	PixQrCode    *string     `json:"pixQrCode,omitempty"`
	PixCopy      *string     `json:"pixCopy,omitempty"`
}

type Split struct {
	ID                 string  `json:"id"`
	WalletID           string  `json:"walletId"`
	FixedValue         float64 `json:"fixedValue"`
	PercentualValue    float64 `json:"percentualValue"`
	TotalValue         float64 `json:"totalValue"`
	CancellationReason string  `json:"cancellationReason"`
	Status             string  `json:"status"`
	ExternalReference  string  `json:"externalReference"`
	Description        string  `json:"description"`
}

const (
	CancellationReasonPaymentDeleted        = "PAYMENT_DELETED"
	CancellationReasonPaymentOverdue        = "PAYMENT_OVERDUE"
	CancellationReasonPaymentReceivedInCash = "PAYMENT_RECEIVED_IN_CASH"
	CancellationReasonPaymentRefunded       = "PAYMENT_REFUNDED"
	CancellationReasonValueDivergenceBlock  = "VALUE_DIVERGENCE_BLOCK"
	CancellationReasonWalletUnableToReceive = "WALLET_UNABLE_TO_RECEIVE"
)

const (
	SplitStatusPending                  = "PENDING"
	SplitStatusProcessing               = "PROCESSING"
	SplitStatusAwaitingCredit           = "AWAITING_CREDIT"
	SplitStatusCancelled                = "CANCELLED"
	SplitStatusDone                     = "DONE"
	SplitStatusRefunded                 = "REFUNDED"
	SplitStatusBlockedByValueDivergence = "BLOCKED_BY_VALUE_DIVERGENCE"
)
