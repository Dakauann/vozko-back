package asaas

type AsaasCustomer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	CPF   string `json:"cpfCnpj"`
}
type AsaasPayment struct {
	ID                                         string         `json:"id,omitempty"`
	Customer                                   string         `json:"customer"`
	BillingType                                string         `json:"billingType"`
	Value                                      float64        `json:"value"`
	DueDate                                    string         `json:"dueDate"`
	Description                                string         `json:"description,omitempty"`
	DaysAfterDueDateToRegistrationCancellation int32          `json:"daysAfterDueDateToRegistrationCancellation,omitempty"`
	ExternalReference                          string         `json:"externalReference,omitempty"`
	InstallmentCount                           int32          `json:"installmentCount,omitempty"`
	TotalValue                                 float64        `json:"totalValue,omitempty"`
	InstallmentValue                           float64        `json:"installmentValue,omitempty"`
	Discount                                   *AsaasDiscount `json:"discount,omitempty"`
	Interest                                   *AsaasInterest `json:"interest,omitempty"`
	Fine                                       *AsaasFine     `json:"fine,omitempty"`
	PostalService                              bool           `json:"postalService,omitempty"`
	Split                                      []AsaasSplit   `json:"split,omitempty"`
	Callback                                   *AsaasCallback `json:"callback,omitempty"`
	BankSlipUrl                                string         `json:"bankSlipUrl,omitempty"`
	InvoiceUrl                                 string         `json:"invoiceUrl,omitempty"`
}

type AsaasDiscount struct {
	Value            float64 `json:"value"`
	DueDateLimitDays int32   `json:"dueDateLimitDays"`
	Type             string  `json:"type"`
}

type AsaasInterest struct {
	Value float64 `json:"value"`
}

type AsaasFine struct {
	Value float64 `json:"value"`
	Type  string  `json:"type"`
}

type AsaasSplit struct {
	WalletID          string  `json:"walletId"`
	FixedValue        float64 `json:"fixedValue"`
	PercentualValue   float64 `json:"percentualValue"`
	TotalFixedValue   float64 `json:"totalFixedValue"`
	ExternalReference string  `json:"externalReference"`
	Description       string  `json:"description"`
}

type AsaasCallback struct {
	SuccessUrl   string `json:"successUrl"`
	AutoRedirect bool   `json:"autoRedirect"`
}
