package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
)

func TestRequirePersonReadsTheCallerFromTheClaims(t *testing.T) {
	cases := []struct {
		name   string
		claims *auth.Claims
		want   shared.Person
		ok     bool
	}{
		{"a member", &auth.Claims{UserID: "u-1", Role: "member"}, shared.Person{UserID: "u-1"}, true},
		{"a platform admin", &auth.Claims{UserID: "u-2", Role: "admin"}, shared.Person{UserID: "u-2", SystemAdmin: true}, true},
		{"no claims", nil, shared.Person{}, false},
		{"claims without a user", &auth.Claims{Role: "member"}, shared.Person{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			if tc.claims != nil {
				req = req.WithContext(context.WithValue(req.Context(), middleware.ClaimsContextKey, tc.claims))
			}
			rec := httptest.NewRecorder()
			got, ok := RequirePerson(rec, req)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("RequirePerson = %+v, %v", got, ok)
			}
			if !ok && rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if fromContext, found := PersonFromContext(req.Context()); found != tc.ok || fromContext != tc.want {
				t.Fatalf("a use case reading the request context sees %+v, %v", fromContext, found)
			}
		})
	}
}
