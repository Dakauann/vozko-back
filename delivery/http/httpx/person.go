package httpx

import (
	"context"
	"net/http"
	"strings"

	"vozko/delivery/http/response"
	"vozko/domain/auth"
	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/infra/http/middleware"
)

func RequirePerson(w http.ResponseWriter, r *http.Request) (shared.Person, bool) {
	person, ok := PersonFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return shared.Person{}, false
	}
	return person, true
}

func PersonFromContext(ctx context.Context) (shared.Person, bool) {
	claims, _ := ctx.Value(middleware.ClaimsContextKey).(*auth.Claims)
	if claims == nil || strings.TrimSpace(claims.UserID) == "" {
		return shared.Person{}, false
	}
	return shared.Person{UserID: claims.UserID, SystemAdmin: claims.Role == string(user.RoleAdmin)}, true
}
