package payment

type AsaasWebhookEvent struct {
	ID      string              `json:"id"`
	Event   string              `json:"event"`
	Payment AsaasWebhookPayment `json:"payment"`
}

type AsaasWebhookPayment struct {
	ID                string  `json:"id"`
	ExternalReference string  `json:"externalReference"`
	BillingType       string  `json:"billingType"`
	Value             float64 `json:"value"`
	Status            string  `json:"status"`

	InvoiceNumber string `json:"invoiceNumber"`
}
