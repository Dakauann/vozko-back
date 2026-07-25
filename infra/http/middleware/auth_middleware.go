package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vozko/delivery/http/response"
	"vozko/domain/auth"
	"vozko/domain/cache"
)

type contextKey string

const ClaimsContextKey contextKey = "claims"

type RoleFetcher interface {
	GetUserRole(userID string) (string, error)
}

type TokenVersionFetcher interface {
	GetTokenVersion(userId string) (int, error)
}

type AuthMiddleware struct {
	verifier       auth.TokenVerifier
	roleFetcher    RoleFetcher
	versionFetcher TokenVersionFetcher
	shared         cache.SharedState
}

func NewAuthMiddleware(verifier auth.TokenVerifier, roleFetcher RoleFetcher, versionFetcher TokenVersionFetcher, shared cache.SharedState) *AuthMiddleware {
	return &AuthMiddleware{verifier: verifier, roleFetcher: roleFetcher, versionFetcher: versionFetcher, shared: shared}
}

func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string

		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				token = parts[1]
			}
		}

		if token == "" {
			token = r.URL.Query().Get("token")
		}

		if token == "" {
			cookie, err := r.Cookie("accessToken")
			if err == nil {
				token = cookie.Value
			}
		}

		if token == "" {
			response.WriteError(w, http.StatusUnauthorized, "Missing authorization", map[string]string{
				"Authorization": "required in header as 'Bearer <token>', as query parameter '?token=<token>', or via accessToken cookie",
			})
			return
		}

		claims, err := m.verifier.Verify(token)
		if err != nil {
			response.WriteError(w, http.StatusUnauthorized, "Invalid or expired token", nil)
			return
		}

		if claims.JTI != "" {
			if revoked, _ := m.shared.Exists("revoked_jti:" + claims.JTI); revoked {
				response.WriteError(w, http.StatusUnauthorized, "Token revoked", nil)
				return
			}
		}

		if m.versionFetcher != nil {
			currentVer, err := m.cachedGetTokenVersion(claims.UserID)
			if err != nil {
				response.WriteError(w, http.StatusUnauthorized, "User not found", nil)
				return
			}
			if claims.TokenVersion < currentVer {
				response.WriteError(w, http.StatusUnauthorized, "Token revoked", nil)
				return
			}
		}

		if m.roleFetcher != nil {
			role, err := m.cachedGetUserRole(claims.UserID)
			if err != nil {
				response.WriteError(w, http.StatusUnauthorized, "User not found", nil)
				return
			}
			claims.Role = role
		}

		r.Header.Set("X-User-ID", claims.UserID)

		ctx := context.WithValue(r.Context(), ClaimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

const roleCacheTTL = 30 * time.Second
const tokenVersionCacheTTL = 30 * time.Second

func (m *AuthMiddleware) cachedGetUserRole(userID string) (string, error) {
	key := "cache:role:" + userID
	if role, err := m.shared.GetString(key); err == nil && role != "" {
		return role, nil
	}

	role, err := m.roleFetcher.GetUserRole(userID)
	if err != nil {
		return "", err
	}

	_ = m.shared.SetString("cache:role:"+userID, role, roleCacheTTL)
	return role, nil
}

func (m *AuthMiddleware) cachedGetTokenVersion(userID string) (int, error) {
	key := "cache:token_ver:" + userID
	if val, err := m.shared.GetString(key); err == nil && val != "" {
		v, err := strconv.Atoi(val)
		if err == nil {
			return v, nil
		}
	}

	ver, err := m.versionFetcher.GetTokenVersion(userID)
	if err != nil {
		return 0, err
	}

	_ = m.shared.SetString(key, fmt.Sprintf("%d", ver), tokenVersionCacheTTL)
	return ver, nil
}

func (m *AuthMiddleware) RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(ClaimsContextKey).(*auth.Claims)
			if !ok || claims == nil {
				response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
				return
			}

			if claims.Role != role {
				response.WriteError(w, http.StatusForbidden, "Forbidden: insufficient permissions", map[string]string{"role": "requires '" + role + "' role"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func GetClaims(r *http.Request) *auth.Claims {
	claims, _ := r.Context().Value(ClaimsContextKey).(*auth.Claims)
	return claims
}
