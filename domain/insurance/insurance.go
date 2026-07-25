package insurance

import "time"

type PolicyType string
type InsuranceProvider string

const (
	InsuranceProviderPottencial InsuranceProvider = "POTTENCIAL"
)

const (
	PolicyTypeJudicialExecutionFiscal PolicyType = "GARANTIA_JUDICIAL_EXECUCAO_FISCAL"
	PolicyTypeImobiliario             PolicyType = "IMOBILIARIO"
)

type Quotation struct {
	ID         string           `json:"id"`
	UserID     string           `json:"user_id"`
	PolicyType PolicyType       `json:"policy_type"`
	Quotes     []InsuranceQuote `json:"quotes"`
	CreatedAt  time.Time        `json:"created_at"`
}

type InsuranceQuote struct {
	ID          string                 `json:"id"`
	QuotationID string                 `json:"quotation_id"`
	ExternalID  string                 `json:"external_id"`
	UserID      string                 `json:"user_id"`
	PolicyType  PolicyType             `json:"policy_type"`
	Provider    InsuranceProvider      `json:"provider"`
	CoverageAmt float64                `json:"coverage_amt"`
	Premium     float64                `json:"premium"`
	Currency    string                 `json:"currency"`
	ValidUntil  *time.Time             `json:"valid_until"`
	CreatedAt   time.Time              `json:"created_at"`
	Metadata    map[string]interface{} `json:"metadata"`
}
