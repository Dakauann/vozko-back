package apikeys

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vozko/delivery/http/response"
	"vozko/domain/apikey"
	"vozko/domain/cache"
	"vozko/infra/http/middleware"
	apikey_usecase "vozko/usecases/apikey"
)

const (
	CodeMissing     = "api_key_missing"
	CodeInvalid     = "api_key_invalid"
	CodeRevoked     = "api_key_revoked"
	CodeScope       = "api_key_scope_missing"
	CodeUnavailable = "api_key_unavailable"
	CodeRateLimited = "api_key_rate_limited"
)

var authRefusals = []response.ErrorRefusal{
	{Err: apikey_usecase.ErrUnauthenticated, Refusal: response.Refusal{Status: http.StatusUnauthorized, Code: CodeMissing, Message: "Envie a chave de API no cabeçalho Authorization: Bearer <chave>."}},
	{Err: apikey_usecase.ErrInvalid, Refusal: response.Refusal{Status: http.StatusUnauthorized, Code: CodeInvalid, Message: "A chave de API é inválida."}},
	{Err: apikey.ErrRevoked, Refusal: response.Refusal{Status: http.StatusUnauthorized, Code: CodeRevoked, Message: "A chave de API foi revogada."}},
	{Err: apikey_usecase.ErrScopeMissing, Refusal: response.Refusal{Status: http.StatusForbidden, Code: CodeScope, Message: "A chave de API não tem permissão para esta rota."}},
	{Err: apikey_usecase.ErrUnavailable, Refusal: response.Refusal{Status: http.StatusServiceUnavailable, Code: CodeUnavailable, Message: "Não foi possível verificar a chave de API agora; tente de novo em instantes."}},
}

var refusalRateLimited = response.Refusal{Status: http.StatusTooManyRequests, Code: CodeRateLimited, Message: "Limite de requisições atingido; aguarde e tente de novo."}

func AuthRefusalCodes() []string {
	return response.RefusalCodes(response.ErrorRefusalsOf(authRefusals), []response.Refusal{refusalRateLimited})
}

type KeyAuthenticator interface {
	Authenticate(ctx context.Context, token string, scope apikey.Scope) (apikey.Principal, error)
}

type Authenticator struct {
	keys   KeyAuthenticator
	perKey cache.RateLimiter
	perIP  cache.RateLimiter
}

func NewAuthenticator(keys KeyAuthenticator, perKey, perIP cache.RateLimiter) *Authenticator {
	if keys == nil || perKey == nil || perIP == nil {
		panic("apikeys: the authenticator needs the key service and both rate limiters")
	}
	return &Authenticator{keys: keys, perKey: perKey, perIP: perIP}
}

type principalKey struct{}

func PrincipalOf(r *http.Request) (apikey.Principal, bool) {
	p, ok := r.Context().Value(principalKey{}).(apikey.Principal)
	return p, ok
}

func bearer(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func (a *Authenticator) limited(w http.ResponseWriter, limiter cache.RateLimiter, key string) bool {
	allowed, retryAfter, err := limiter.Allow(key)
	if err == nil && allowed {
		return false
	}
	if err != nil {
		log.Printf("[api-keys] rate limiter unavailable, refusing: %v", err)
		retryAfter = time.Second
	}
	seconds := int64(retryAfter.Seconds() + 0.999)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	response.WriteErrorWithDetails(w, refusalRateLimited.Status, refusalRateLimited.Code, refusalRateLimited.Message, map[string]any{"retry_after_seconds": seconds})
	return true
}

func (a *Authenticator) Require(scope apikey.Scope, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.limited(w, a.perIP, middleware.GetClientIP(r)) {
			return
		}
		principal, err := a.keys.Authenticate(r.Context(), bearer(r), scope)
		if err != nil {
			if !response.WriteErrorRefusal(w, err, authRefusals) {
				log.Printf("[api-keys] authentication failed: %v", err)
				response.WriteRefusal(w, authRefusals[len(authRefusals)-1].Refusal)
			}
			return
		}
		if a.limited(w, a.perKey, principal.KeyID) {
			return
		}
		ctx := context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, principal.WorkspaceID)
		ctx = context.WithValue(ctx, principalKey{}, principal)
		r.Header.Del("X-Workspace-ID")
		next(w, r.WithContext(ctx))
	}
}
