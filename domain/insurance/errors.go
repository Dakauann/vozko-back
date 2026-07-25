package insurance

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNoProvidersForPolicy    = errors.New("insurance: no providers available for policy type")
	ErrMissingRequiredFields   = errors.New("insurance: missing required fields")
	ErrUnsupportedPolicyType   = errors.New("insurance: unsupported policy type")
	ErrInvalidQuoteRequest     = errors.New("insurance: invalid quote request")
	ErrRepositoryNotConfigured = errors.New("insurance: repository not configured")
	ErrQuotationNotFound       = errors.New("insurance: quotation not found")
)

type FieldViolation struct {
	Field  RequiredField
	Reason string
}

type MissingRequiredFieldsError struct {
	PolicyType PolicyType
	Violations []FieldViolation
}

func (e *MissingRequiredFieldsError) Error() string {
	if e == nil {
		return ErrMissingRequiredFields.Error()
	}
	paths := make([]string, 0, len(e.Violations))
	for _, v := range e.Violations {
		paths = append(paths, v.Field.Path)
	}
	return fmt.Sprintf("missing required fields for policy %s: %s", e.PolicyType, strings.Join(paths, ", "))
}

func (e *MissingRequiredFieldsError) Is(target error) bool {
	return target == ErrMissingRequiredFields
}

func (e *MissingRequiredFieldsError) MissingFields() []FieldViolation {
	if e == nil {
		return nil
	}
	return e.Violations
}
