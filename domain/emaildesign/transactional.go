package emaildesign

import (
	"strings"

	"vozko/domain/lead"
)

func ExpandText(text string, p Personalization) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", &ValidationError{Kind: ErrInvalidDocument, Issues: []Issue{{Code: issueTextEmpty, Field: "text"}}}
	}
	return expander{person: p}.expand(text, identity, identity)
}

func LeadPersonalization(f lead.EmailFacts, email string, variables map[string]string) Personalization {
	fields := f.FieldValues()
	for key, value := range variables {
		fields[key] = value
	}
	return Personalization{
		FirstName: f.FirstName(),
		Name:      f.FullName(),
		Email:     email,
		Company:   f.Company,
		Fields:    fields,
	}
}
