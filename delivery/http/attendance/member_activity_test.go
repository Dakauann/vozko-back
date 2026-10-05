package attendance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	workspace_domain "vozko/domain/workspace"
	dept "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
	attendance_usecase "vozko/usecases/attendance"
)

type activityReaderStub struct {
	query attendance_usecase.MemberActivityQuery
	err   error
}

func (s *activityReaderStub) Execute(_ context.Context, q attendance_usecase.MemberActivityQuery) (*attendance_usecase.MemberActivityReport, error) {
	s.query = q
	if s.err != nil {
		return nil, s.err
	}
	return &attendance_usecase.MemberActivityReport{}, nil
}

var viewerFilter = &dept.DepartmentFilter{DepartmentIDs: []string{"vendas"}, WorkspaceHasDepartments: true}

func activityRequest(path string, vars map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r = mux.SetURLVars(r, vars)
	ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "viewer"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws")
	ctx = context.WithValue(ctx, middleware.DepartmentFilterContextKey, viewerFilter)
	return r.WithContext(ctx)
}

func activityHandler(reader memberActivityReader) *AttendanceHandler {
	h := &AttendanceHandler{}
	h.SetMemberActivity(reader)
	return h
}

func TestAManagerAsksForAMembersActivityWithTheirDepartmentScope(t *testing.T) {
	reader := &activityReaderStub{}
	rec := httptest.NewRecorder()
	activityHandler(reader).GetMemberActivity(rec, activityRequest("/attendance/members/marina/activity?date_from=2026-09-01&date_to=2026-09-30&timezone=America/Sao_Paulo", map[string]string{"id": "marina"}))
	q := reader.query
	if rec.Code != http.StatusOK || q.Self || q.MemberID != "marina" || q.WorkspaceID != "ws" || q.FromDay != "2026-09-01" || q.ToDay != "2026-09-30" || q.Timezone != "America/Sao_Paulo" || q.Departments != viewerFilter {
		t.Fatalf("status %d query %+v", rec.Code, q)
	}
}

func TestMyActivityIsAlwaysTheCallersOwn(t *testing.T) {
	reader := &activityReaderStub{}
	rec := httptest.NewRecorder()
	activityHandler(reader).GetMyActivity(rec, activityRequest("/attendance/members/me/activity?member_id=someone-else", nil))
	if rec.Code != http.StatusOK || !reader.query.Self || reader.query.MemberID != "viewer" {
		t.Fatalf("status %d query %+v", rec.Code, reader.query)
	}
}

func TestMemberActivityErrorsMapToStatuses(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{attendance_usecase.ErrMemberOutOfScope, http.StatusNotFound},
		{attendance_usecase.ErrActivityPeriod, http.StatusBadRequest},
		{errors.New("db down"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		activityHandler(&activityReaderStub{err: tc.err}).GetMemberActivity(rec, activityRequest("/x", map[string]string{"id": "marina"}))
		if rec.Code != tc.want {
			t.Errorf("%v: status %d", tc.err, rec.Code)
		}
	}
}

func TestMemberActivityRefusesWhenUnavailable(t *testing.T) {
	rec := httptest.NewRecorder()
	(&AttendanceHandler{}).GetMyActivity(rec, activityRequest("/x", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestMyActivityRouteIsNotMistakenForAMemberID(t *testing.T) {
	router := mux.NewRouter()
	refuse := func(_ workspace_domain.Resource, _ workspace_domain.Action, _ http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }
	}
	member := func(next http.HandlerFunc) http.HandlerFunc { return next }
	reader := &activityReaderStub{}
	RegisterProtectedRoutes(router, activityHandler(reader), refuse, member)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, activityRequest("/attendance/members/me/activity", nil))
	if rec.Code != http.StatusOK || !reader.query.Self {
		t.Fatalf("status %d query %+v", rec.Code, reader.query)
	}
}
