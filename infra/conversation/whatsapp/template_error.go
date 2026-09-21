package whatsapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"vozko/domain/whatsapp/template"
	"vozko/infra/meta"
)

type TemplateAPIError struct {
	Op   string
	Meta *meta.Error
}

func (e *TemplateAPIError) Error() string {
	if e == nil {
		return "whatsapp: <nil>"
	}
	if e.Meta == nil {
		return fmt.Sprintf("whatsapp %s: unknown provider error", e.Op)
	}
	return fmt.Sprintf("whatsapp %s: %s", e.Op, e.Meta.Error())
}

func (e *TemplateAPIError) Unwrap() error {
	if e == nil || e.Meta == nil {
		return nil
	}
	return e.Meta
}

func (e *TemplateAPIError) UserMessage() string {
	if e == nil || e.Meta == nil {
		return ""
	}
	for _, candidate := range []string{e.Meta.UserMsg, e.Meta.UserTitle, e.Meta.Message} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (e *TemplateAPIError) Retryable() bool {
	return e != nil && e.Meta != nil && e.Meta.Retryable()
}

func newTemplateAPIError(op string, status int, body []byte) *TemplateAPIError {
	parsed := &meta.Error{HTTPStatus: status}

	var envelope struct {
		Error meta.Error `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelopeIsPopulated(envelope.Error) {
		decoded := envelope.Error
		decoded.HTTPStatus = status
		parsed = &decoded
	} else {
		parsed.Message = truncateForMessage(string(body))
	}

	return &TemplateAPIError{Op: op, Meta: parsed}
}

func envelopeIsPopulated(e meta.Error) bool {
	return e.Code != 0 || e.Message != "" || e.UserMsg != "" || e.UserTitle != "" || e.Type != ""
}

const maxRawMessage = 300

func truncateForMessage(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxRawMessage {
		return s
	}
	return s[:maxRawMessage] + "…"
}

func AsTemplateAPIError(err error) (*TemplateAPIError, bool) {
	var apiErr *TemplateAPIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

func IsProviderUnavailable(err error) bool {
	apiErr, ok := AsTemplateAPIError(err)
	if !ok {
		return false
	}
	return apiErr.ProviderUnavailable()
}

func (e *TemplateAPIError) ProviderUnavailable() bool {
	if e == nil {
		return false
	}
	if e.Retryable() {
		return true
	}
	return e.Meta != nil && e.Meta.HTTPStatus >= http.StatusInternalServerError
}

var _ template.ProviderError = (*TemplateAPIError)(nil)
