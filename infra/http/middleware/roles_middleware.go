package middleware

import (
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/user"
)

type RolesMiddleware struct{}

func NewRolesMiddleware() *RolesMiddleware {
	return &RolesMiddleware{}
}

func (m *RolesMiddleware) RequireRole(role user.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaims(r)
			if claims == nil {
				response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
				return
			}

			if claims.Role != string(role) {
				response.WriteError(w, http.StatusForbidden, "Forbidden: insufficient permissions", map[string]string{
					"role": "requires '" + string(role) + "' role",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (m *RolesMiddleware) RequireAnyRole(roles ...user.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaims(r)
			if claims == nil {
				response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
				return
			}

			hasRole := false
			for _, role := range roles {
				if claims.Role == string(role) {
					hasRole = true
					break
				}
			}

			if !hasRole {
				roleNames := make([]string, len(roles))
				for i, role := range roles {
					roleNames[i] = string(role)
				}
				response.WriteError(w, http.StatusForbidden, "Forbidden: insufficient permissions", map[string]string{
					"role": "requires one of: " + joinRolesStrings(roleNames, ", "),
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func joinRolesStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
