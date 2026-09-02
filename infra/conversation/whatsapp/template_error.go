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

// TemplateAPIError is a template call that Meta refused.
//
// It exists because the template client used to return
// fmt.Errorf("... status=%d body=%s", status, body) — the whole Graph error
// envelope, raw JSON, as the error string. That string travelled unchanged to a
// toast, so an operator creating a template saw a wall of
// {"error":{"message":...,"fbtrace_id":...}} and learned nothing from it.
//
// Meta already ships the sentence we want: error_user_msg is written for end
// users and returned in the locale of the calling token. Keeping the envelope
// structured is what lets the HTTP layer show that sentence, classify the
// failure, and keep the trace id in the log where it belongs.
//
// Meta is a named field rather than embedded: embedding would promote
// meta.Error's own Error() method and collide with this type's.
type TemplateAPIError struct {
	// Op is the call that failed ("create template", "upload media"), so a log
	// line says which without anyone parsing the message.
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

// UserMessage is the sentence to put in front of the operator.
//
// The preference order is deliberate. error_user_msg is Meta's own end-user
// copy, returned in the locale of the calling token; error_user_title is its
// shorter form; message is the developer string — English, and usually about
// the request shape rather than about the template. When all three are empty we
// return "" so the caller can say something honest, instead of showing an HTTP
// status as though it were an explanation.
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

// Retryable reports whether the call could plausibly succeed on a retry, which
// is also what separates "our problem" from "this template is wrong".
func (e *TemplateAPIError) Retryable() bool {
	return e != nil && e.Meta != nil && e.Meta.Retryable()
}

// newTemplateAPIError parses Meta's error envelope out of a non-2xx response.
//
// A body that is not the expected envelope still produces a TemplateAPIError,
// carrying the raw body (truncated) as the message. Losing the classification
// is acceptable; losing the error is not, and an unparseable body from Meta is
// exactly when a caller most needs to see something.
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

// envelopeIsPopulated guards against a body that unmarshals into an all-zero
// Error — an empty object, or JSON with no "error" member at all. Treating that
// as parsed would discard a raw body we could still have shown.
func envelopeIsPopulated(e meta.Error) bool {
	return e.Code != 0 || e.Message != "" || e.UserMsg != "" || e.UserTitle != "" || e.Type != ""
}

// maxRawMessage bounds an unparseable body. Meta can answer with an HTML error
// page, and putting that in a toast is worse than saying nothing.
const maxRawMessage = 300

func truncateForMessage(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxRawMessage {
		return s
	}
	return s[:maxRawMessage] + "…"
}

// AsTemplateAPIError extracts a *TemplateAPIError from an error chain.
func AsTemplateAPIError(err error) (*TemplateAPIError, bool) {
	var apiErr *TemplateAPIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

// IsProviderUnavailable reports a failure that is ours rather than the
// operator's: a 5xx, a rate limit, or something Meta itself flagged transient.
// Those deserve a retry and a 502, not a "fix your template" message.
func IsProviderUnavailable(err error) bool {
	apiErr, ok := AsTemplateAPIError(err)
	if !ok {
		return false
	}
	return apiErr.ProviderUnavailable()
}

// ProviderUnavailable implements the domain's ProviderError port: it reports a
// failure that is ours rather than the operator's — a 5xx, a rate limit, or
// something Meta itself flagged transient. Those deserve a retry, not a "fix
// your template" message.
func (e *TemplateAPIError) ProviderUnavailable() bool {
	if e == nil {
		return false
	}
	if e.Retryable() {
		return true
	}
	return e.Meta != nil && e.Meta.HTTPStatus >= http.StatusInternalServerError
}

// Compile-time proof that the concrete error satisfies the domain port. Without
// it, a signature drift would only show up as a provider rejection quietly
// falling through to a 500.
var _ template.ProviderError = (*TemplateAPIError)(nil)
