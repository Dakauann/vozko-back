package httpx

import (
	"context"
	"testing"

	"vozko/domain/auth"
	dept "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
)

func TestWithCreationScopeNamesTheCallerAndTheDepartment(t *testing.T) {
	selected := "dept-filter"
	base := requestAs(&auth.Claims{UserID: "ana", Role: "admin"}, "ws-1")
	filtered := base.WithContext(context.WithValue(base.Context(), middleware.DepartmentFilterContextKey, &dept.DepartmentFilter{SelectedDepartmentID: &selected}))
	cases := []struct {
		name     string
		explicit string
		want     dept.CreationScope
	}{
		{name: "the department of the filter", want: dept.CreationScope{UserID: "ana", IsSystemAdmin: true, RequestedDepartmentID: "dept-filter"}},
		{name: "an explicit department wins", explicit: " dept-body ", want: dept.CreationScope{UserID: "ana", IsSystemAdmin: true, RequestedDepartmentID: "dept-body"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope, ok := dept.GetCreationScope(WithCreationScope(filtered, tc.explicit).Context())
			if !ok || scope != tc.want {
				t.Fatalf("scope = %+v %v, want %+v", scope, ok, tc.want)
			}
		})
	}
	if WithCreationScope(nil, "") != nil {
		t.Fatal("a nil request must stay nil")
	}
}
