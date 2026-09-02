package whatsapp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/whatsapp/template"
)

// The whole point of the type: Meta's own end-user sentence reaches the
// operator instead of the JSON envelope it arrived in.
func TestTemplateAPIError_PrefersMetasUserFacingMessage(t *testing.T) {
	body := []byte(`{"error":{
		"message":"(#100) Invalid parameter",
		"type":"OAuthException",
		"code":100,
		"error_subcode":2388023,
		"error_user_title":"Nome de modelo já existe",
		"error_user_msg":"Já existe um modelo com esse nome. Escolha outro nome.",
		"fbtrace_id":"AbC123"
	}}`)

	apiErr := newTemplateAPIError("create template", http.StatusBadRequest, body)

	assert.Equal(t, "Já existe um modelo com esse nome. Escolha outro nome.", apiErr.UserMessage(),
		"error_user_msg is Meta's localised end-user copy and outranks the developer message")
	assert.Equal(t, 100, apiErr.Meta.Code)
	assert.Equal(t, "AbC123", apiErr.Meta.FBTraceID, "the trace id stays available for logs")
}

func TestTemplateAPIError_FallsBackThroughTitleThenDeveloperMessage(t *testing.T) {
	onlyTitle := newTemplateAPIError("create template", 400,
		[]byte(`{"error":{"code":100,"error_user_title":"Modelo inválido"}}`))
	assert.Equal(t, "Modelo inválido", onlyTitle.UserMessage())

	onlyMessage := newTemplateAPIError("create template", 400,
		[]byte(`{"error":{"code":100,"message":"(#100) Invalid parameter"}}`))
	assert.Equal(t, "(#100) Invalid parameter", onlyMessage.UserMessage(),
		"the developer string is a poor last resort, but better than nothing")
}

// An unparseable body must still produce a usable error. This is exactly when a
// caller most needs to see something — and where the old code at least had the
// raw body, so the replacement must not do worse.
func TestTemplateAPIError_KeepsAnUnparseableBody(t *testing.T) {
	apiErr := newTemplateAPIError("create template", 502, []byte("<html>Bad Gateway</html>"))

	assert.Contains(t, apiErr.UserMessage(), "Bad Gateway")
	assert.Equal(t, 502, apiErr.Meta.HTTPStatus)
}

// Meta can answer with a whole HTML error page; that must not end up in a toast.
func TestTemplateAPIError_TruncatesAHugeBody(t *testing.T) {
	apiErr := newTemplateAPIError("create template", 500, []byte(strings.Repeat("x", 5000)))

	assert.LessOrEqual(t, len([]rune(apiErr.UserMessage())), maxRawMessage+1)
	assert.True(t, strings.HasSuffix(apiErr.UserMessage(), "…"))
}

// An empty envelope must not be mistaken for a parsed one, or a body we could
// have shown gets thrown away for nothing.
func TestTemplateAPIError_EmptyEnvelopeFallsBackToTheRawBody(t *testing.T) {
	apiErr := newTemplateAPIError("create template", 400, []byte(`{"error":{}}`))
	assert.Equal(t, `{"error":{}}`, apiErr.UserMessage())
}

// A provider with nothing useful to say yields "", so the caller can be honest
// rather than printing a status code as if it explained anything.
func TestTemplateAPIError_NoMessageAtAllYieldsEmpty(t *testing.T) {
	apiErr := &TemplateAPIError{Op: "create template", Meta: nil}
	assert.Equal(t, "", apiErr.UserMessage())
	assert.False(t, apiErr.ProviderUnavailable())
	assert.Contains(t, apiErr.Error(), "create template")
}

// ── classification ──────────────────────────────────────────────────────────

// The line that decides whether the operator is told to fix their template or
// to try again.
func TestTemplateAPIError_ClassifiesOursVersusTheirs(t *testing.T) {
	rejected := newTemplateAPIError("create template", 400,
		[]byte(`{"error":{"code":100,"message":"Invalid parameter"}}`))
	assert.False(t, rejected.ProviderUnavailable(),
		"a 400 is the template being wrong, which the operator can fix")

	serverError := newTemplateAPIError("create template", 500,
		[]byte(`{"error":{"code":2,"message":"Service temporarily unavailable"}}`))
	assert.True(t, serverError.ProviderUnavailable(), "a 5xx is ours")

	rateLimited := newTemplateAPIError("create template", 429,
		[]byte(`{"error":{"code":4,"message":"Application request limit reached"}}`))
	assert.True(t, rateLimited.ProviderUnavailable(), "a rate limit is ours and retryable")

	transient := newTemplateAPIError("create template", 400,
		[]byte(`{"error":{"code":100,"is_transient":true,"message":"try later"}}`))
	assert.True(t, transient.ProviderUnavailable(),
		"Meta's own is_transient flag outranks the status code")
}

// ── plumbing ────────────────────────────────────────────────────────────────

// The HTTP layer classifies through the DOMAIN port, never by importing this
// package. If the concrete type stops satisfying it, provider rejections
// silently become 500s.
func TestTemplateAPIError_SatisfiesTheDomainPort(t *testing.T) {
	var err error = newTemplateAPIError("create template", 400,
		[]byte(`{"error":{"code":100,"error_user_msg":"Corrija o modelo"}}`))

	providerErr, ok := template.AsProviderError(err)
	require.True(t, ok, "the domain must be able to recognise this without importing infra")
	assert.Equal(t, "Corrija o modelo", providerErr.UserMessage())
	assert.False(t, providerErr.ProviderUnavailable())
}

// Classification has to survive wrapping — the usecase returns the client's
// error up through several layers.
func TestTemplateAPIError_SurvivesWrapping(t *testing.T) {
	inner := newTemplateAPIError("create template", 503, []byte(`{"error":{"code":2}}`))
	wrapped := fmt.Errorf("creating template %q: %w", "promo_natal", inner)

	providerErr, ok := template.AsProviderError(wrapped)
	require.True(t, ok)
	assert.True(t, providerErr.ProviderUnavailable())

	assert.True(t, errors.Is(wrapped, inner.Meta), "the meta.Error stays reachable for retry logic")
}

// A plain error is not a provider error; misreading one as a rejection would
// show an operator "WhatsApp refused this" for a bug in our own code.
func TestTemplateAPIError_PlainErrorIsNotAProviderError(t *testing.T) {
	_, ok := template.AsProviderError(errors.New("database is down"))
	assert.False(t, ok)
	assert.False(t, IsProviderUnavailable(errors.New("database is down")))
}
