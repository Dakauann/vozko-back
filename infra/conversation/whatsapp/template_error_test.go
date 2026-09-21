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

func TestTemplateAPIError_KeepsAnUnparseableBody(t *testing.T) {
	apiErr := newTemplateAPIError("create template", 502, []byte("<html>Bad Gateway</html>"))

	assert.Contains(t, apiErr.UserMessage(), "Bad Gateway")
	assert.Equal(t, 502, apiErr.Meta.HTTPStatus)
}

func TestTemplateAPIError_TruncatesAHugeBody(t *testing.T) {
	apiErr := newTemplateAPIError("create template", 500, []byte(strings.Repeat("x", 5000)))

	assert.LessOrEqual(t, len([]rune(apiErr.UserMessage())), maxRawMessage+1)
	assert.True(t, strings.HasSuffix(apiErr.UserMessage(), "…"))
}

func TestTemplateAPIError_EmptyEnvelopeFallsBackToTheRawBody(t *testing.T) {
	apiErr := newTemplateAPIError("create template", 400, []byte(`{"error":{}}`))
	assert.Equal(t, `{"error":{}}`, apiErr.UserMessage())
}

func TestTemplateAPIError_NoMessageAtAllYieldsEmpty(t *testing.T) {
	apiErr := &TemplateAPIError{Op: "create template", Meta: nil}
	assert.Equal(t, "", apiErr.UserMessage())
	assert.False(t, apiErr.ProviderUnavailable())
	assert.Contains(t, apiErr.Error(), "create template")
}

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

func TestTemplateAPIError_SatisfiesTheDomainPort(t *testing.T) {
	var err error = newTemplateAPIError("create template", 400,
		[]byte(`{"error":{"code":100,"error_user_msg":"Corrija o modelo"}}`))

	providerErr, ok := template.AsProviderError(err)
	require.True(t, ok, "the domain must be able to recognise this without importing infra")
	assert.Equal(t, "Corrija o modelo", providerErr.UserMessage())
	assert.False(t, providerErr.ProviderUnavailable())
}

func TestTemplateAPIError_SurvivesWrapping(t *testing.T) {
	inner := newTemplateAPIError("create template", 503, []byte(`{"error":{"code":2}}`))
	wrapped := fmt.Errorf("creating template %q: %w", "promo_natal", inner)

	providerErr, ok := template.AsProviderError(wrapped)
	require.True(t, ok)
	assert.True(t, providerErr.ProviderUnavailable())

	assert.True(t, errors.Is(wrapped, inner.Meta), "the meta.Error stays reachable for retry logic")
}

func TestTemplateAPIError_PlainErrorIsNotAProviderError(t *testing.T) {
	_, ok := template.AsProviderError(errors.New("database is down"))
	assert.False(t, ok)
	assert.False(t, IsProviderUnavailable(errors.New("database is down")))
}
