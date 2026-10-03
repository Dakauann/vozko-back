package advertising

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type FieldIssue struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

type ValidationError struct {
	Issues  []FieldIssue
	Minimum *BudgetMinimum
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		parts = append(parts, issue.Field+":"+issue.Code)
	}
	return "advertising: invalid input (" + strings.Join(parts, ", ") + ")"
}

type issues struct {
	prefix string
	out    *ValidationError
}

func newIssues() issues { return issues{out: &ValidationError{}} }

func (i issues) at(segment string) issues {
	if i.prefix == "" {
		return issues{prefix: segment, out: i.out}
	}
	return issues{prefix: i.prefix + "." + segment, out: i.out}
}

func (i issues) item(segment string, index int) issues {
	return i.at(segment + "[" + strconv.Itoa(index) + "]")
}

func (i issues) add(field, code string) {
	path := field
	if i.prefix != "" && field != "" {
		path = i.prefix + "." + field
	} else if i.prefix != "" {
		path = i.prefix
	}
	i.out.Issues = append(i.out.Issues, FieldIssue{Field: path, Code: code})
}

func (i issues) err() error {
	if len(i.out.Issues) == 0 {
		return nil
	}
	return i.out
}

func (i issues) text(field, value string, required bool, max int) {
	switch n := utf8.RuneCountInString(strings.TrimSpace(value)); {
	case n == 0 && required:
		i.add(field, "required")
	case n > max:
		i.add(field, "too_long")
	}
}

func (i issues) url(field, value string, required bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		if required {
			i.add(field, "required")
		}
		return
	}
	if !ValidHTTPSURL(trimmed) {
		i.add(field, "invalid_url")
	}
}

func FieldError(field, code string) *ValidationError {
	return &ValidationError{Issues: []FieldIssue{{Field: field, Code: code}}}
}
