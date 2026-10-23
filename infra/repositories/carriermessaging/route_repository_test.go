package carriermessaging_repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/sms"
)

var routeColumns = []string{"id", "workspace_id", "brand_prefix", "kind", "number", "status", "pause_reason", "created_at", "updated_at", "deleted_at",
	"agent_id", "workflow_id", "enable_agent_responses", "enable_workflow", "enable_analysis", "enable_auto_staging", "enable_auto_memory", "automation_disclosure"}

func routeRows(kind, status string) *sqlmock.Rows {
	return sqlmock.NewRows(routeColumns).AddRow("r1", "ws1", "Loja", kind, "29123", status, "", testNow, testNow, nil,
		nil, "wf1", false, true, false, true, true, "")
}

func TestRouteCreateWritesBackIDAndTimes(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(`INSERT INTO "sms_routes"`).
		WithArgs(sqlmock.AnyArg(), "ws1", "Loja", "dedicated", "29123", "active", "",
			"a1", nil, true, false, true, false, false, "Sou um assistente", nil, nil,
			sqlmock.AnyArg(), sqlmock.AnyArg(), nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	agent := "a1"
	r := &sms.Route{WorkspaceID: "ws1", BrandPrefix: "Loja", Kind: sms.RouteDedicated, Number: "29123", Status: sms.RouteActive,
		Automation: conversation.ChannelAutomation{AgentID: &agent, EnableAgentResponses: true, EnableAnalysis: true, Disclosure: "Sou um assistente"}}
	if err := NewRouteRepository(db).Create(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if r.ID == "" || r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		t.Fatalf("got %+v", r)
	}
	expectationsMet(t, mock)
}

func TestRouteWritesRefuseUnknownValues(t *testing.T) {
	cases := []struct {
		name  string
		route sms.Route
	}{
		{"kind", sms.Route{WorkspaceID: "ws1", Kind: "private", Status: sms.RouteActive}},
		{"status", sms.Route{WorkspaceID: "ws1", Kind: sms.RouteShared, Status: "live"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			repo := NewRouteRepository(db)
			if err := repo.Create(context.Background(), &tc.route); !errors.Is(err, errUnknownStoredValue) {
				t.Fatalf("create: %v", err)
			}
			if err := repo.Update(context.Background(), &tc.route); !errors.Is(err, errUnknownStoredValue) {
				t.Fatalf("update: %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestRouteUpdateIsScopedToTheWorkspace(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		want     error
	}{
		{"updated", 1, nil},
		{"missing", 0, sms.ErrRouteNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(`UPDATE "sms_routes" SET .*"pause_reason"=.* WHERE workspace_id = \$\d+ AND "sms_routes"."deleted_at" IS NULL AND "id" = \$\d+`).
				WillReturnResult(sqlmock.NewResult(0, tc.affected))
			r := &sms.Route{ID: "r1", WorkspaceID: "ws1", BrandPrefix: "Loja", Kind: sms.RouteShared, Status: sms.RouteActive}
			if err := NewRouteRepository(db).Update(context.Background(), r); !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestRouteFinders(t *testing.T) {
	cases := []struct {
		name  string
		query string
		args  []driver.Value
		run   func(carriermessaging.RouteRepository) (*sms.Route, error)
	}{
		{"by id", `SELECT \* FROM "sms_routes" WHERE \(id = \$1 AND workspace_id = \$2\) AND "sms_routes"."deleted_at" IS NULL`, []driver.Value{"r1", "ws1", 1},
			func(r carriermessaging.RouteRepository) (*sms.Route, error) {
				return r.FindByID(context.Background(), "ws1", "r1")
			}},
		{"unscoped", `SELECT \* FROM "sms_routes" WHERE id = \$1 AND "sms_routes"."deleted_at" IS NULL`, []driver.Value{"r1", 1},
			func(r carriermessaging.RouteRepository) (*sms.Route, error) {
				return r.FindByIDUnscoped(context.Background(), "r1")
			}},
		{"by number", `SELECT \* FROM "sms_routes" WHERE \(kind = \$1 AND number = \$2\) AND "sms_routes"."deleted_at" IS NULL`, []driver.Value{"dedicated", "29123", 1},
			func(r carriermessaging.RouteRepository) (*sms.Route, error) {
				return r.FindByNumber(context.Background(), "29123")
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(tc.query).WithArgs(tc.args...).WillReturnRows(routeRows("dedicated", "active"))
			got, err := tc.run(NewRouteRepository(db))
			if err != nil || got.ID != "r1" || got.Kind != sms.RouteDedicated || got.Status != sms.RouteActive || got.Number != "29123" ||
				got.Automation.AgentID != nil || got.Automation.WorkflowID == nil || *got.Automation.WorkflowID != "wf1" ||
				!got.Automation.EnableWorkflow || !got.Automation.EnableAutoStaging || !got.Automation.EnableAutoMemory || got.Automation.EnableAnalysis {
				t.Fatalf("got %+v %v", got, err)
			}
			expectationsMet(t, mock)

			db, mock = newMockDB(t)
			mock.ExpectQuery(tc.query).WillReturnRows(sqlmock.NewRows(routeColumns))
			if _, err := tc.run(NewRouteRepository(db)); !errors.Is(err, sms.ErrRouteNotFound) {
				t.Fatalf("missing route: %v", err)
			}
		})
	}
}

func TestRouteReadsFailClosedOnUnknownStoredValues(t *testing.T) {
	cases := []struct{ name, kind, status string }{
		{"kind", "private", "active"},
		{"status", "shared", "live"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(`SELECT \* FROM "sms_routes"`).WillReturnRows(routeRows(tc.kind, tc.status))
			if got, err := NewRouteRepository(db).FindByIDUnscoped(context.Background(), "r1"); !errors.Is(err, errUnknownStoredValue) || got != nil {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}

func TestRouteLists(t *testing.T) {
	cases := []struct {
		name  string
		query string
		run   func(carriermessaging.RouteRepository) ([]*sms.Route, error)
	}{
		{"by workspace", `SELECT \* FROM "sms_routes" WHERE workspace_id = \$1 AND "sms_routes"."deleted_at" IS NULL ORDER BY created_at DESC`,
			func(r carriermessaging.RouteRepository) ([]*sms.Route, error) {
				return r.ListByWorkspace(context.Background(), "ws1")
			}},
		{"all", `SELECT \* FROM "sms_routes" WHERE "sms_routes"."deleted_at" IS NULL ORDER BY created_at DESC`,
			func(r carriermessaging.RouteRepository) ([]*sms.Route, error) { return r.ListAll(context.Background()) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(tc.query).WillReturnRows(routeRows("shared", "paused"))
			got, err := tc.run(NewRouteRepository(db))
			if err != nil || len(got) != 1 || got[0].Status != sms.RoutePaused {
				t.Fatalf("got %v %v", got, err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestRouteDeleteIsSoftAndScoped(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		want     error
	}{
		{"deleted", 1, nil},
		{"missing", 0, sms.ErrRouteNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(`UPDATE "sms_routes" SET "deleted_at"=\$1 WHERE \(id = \$2 AND workspace_id = \$3\) AND "sms_routes"."deleted_at" IS NULL`).
				WithArgs(sqlmock.AnyArg(), "r1", "ws1").WillReturnResult(sqlmock.NewResult(0, tc.affected))
			if err := NewRouteRepository(db).Delete(context.Background(), "ws1", "r1"); !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}
