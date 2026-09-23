package balance_usecase

import (
	"strings"
	"time"

	balancedomain "vozko/domain/balance"
)

type FilterFieldError struct {
	Field   string
	Message string
}

func (e FilterFieldError) Error() string {
	return e.Field + ": " + e.Message
}

func (e FilterFieldError) ValidationDetails() map[string]string {
	return map[string]string{e.Field: e.Message}
}

type TransactionsFilter struct {
	WorkspaceID string
	ServiceType string
	Type        string
	StartDate   string
	EndDate     string
}

func BuildTransactionsFilter(filter TransactionsFilter) (balancedomain.ListTransactionsInput, error) {
	input := balancedomain.ListTransactionsInput{WorkspaceID: filter.WorkspaceID}

	if raw := strings.TrimSpace(filter.ServiceType); raw != "" {
		serviceType := balancedomain.ServiceType(raw)
		if !serviceType.IsValid() {
			return input, FilterFieldError{Field: "serviceType", Message: "invalid service type"}
		}
		input.ServiceType = &serviceType
	}

	if raw := strings.TrimSpace(filter.Type); raw != "" {
		transactionType := balancedomain.TransactionType(raw)
		if transactionType != balancedomain.TransactionTypeCredit &&
			transactionType != balancedomain.TransactionTypeDebit {
			return input, FilterFieldError{Field: "type", Message: "must be credit or debit"}
		}
		input.Type = &transactionType
	}

	start, err := parseFilterDate("startDate", filter.StartDate)
	if err != nil {
		return input, err
	}
	input.StartDate = start

	end, err := parseFilterDate("endDate", filter.EndDate)
	if err != nil {
		return input, err
	}
	input.EndDate = end

	if start != nil && end != nil && end.Before(*start) {
		return input, FilterFieldError{Field: "endDate", Message: "must not be before startDate"}
	}

	return input, nil
}

func parseFilterDate(field, raw string) (*time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return nil, FilterFieldError{Field: field, Message: "must be an RFC3339 timestamp"}
	}
	return &parsed, nil
}
