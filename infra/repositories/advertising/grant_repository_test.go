package advertising_repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/advertising"
	"vozko/domain/facebook"
)

func TestGrantUpsertCreatesWhenAbsent(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_grants" WHERE (workspace_id = $1 AND app_scoped_user_id = $2 AND client_business_id = $3)`)).
		WithArgs("ws", "asid", "biz", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "ad_grants"`)).WillReturnResult(sqlmock.NewResult(0, 1))

	g := &advertising.Grant{WorkspaceID: "ws", AppScopedUserID: "asid", ClientBusinessID: "biz", Scopes: []string{"ads_read"}}
	if err := NewGrantRepository(db).Upsert(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	if g.ID == "" {
		t.Fatal("new grant id not propagated")
	}
	expectationsMet(t, mock)
}

func TestGrantUpsertRefreshesExistingAndReactivates(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_grants"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id"}).AddRow("g-1", "ws"))
	mock.ExpectExec(`UPDATE "ad_grants" SET .*"revoked_at"=\$5,"scopes"=\$6,"status"=\$7`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), nil, "ads_management,ads_read", "ACTIVE",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "g-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	g := &advertising.Grant{WorkspaceID: "ws", AppScopedUserID: "asid", Scopes: []string{"ads_management", "ads_read"}}
	if err := NewGrantRepository(db).Upsert(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	if g.ID != "g-1" {
		t.Fatalf("id = %q", g.ID)
	}
	expectationsMet(t, mock)
}

func TestGrantUpsertWithoutWorkspaceRunsNoQuery(t *testing.T) {
	db, mock := newMockDB(t)
	err := NewGrantRepository(db).Upsert(context.Background(), &advertising.Grant{AppScopedUserID: "asid"})
	if !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestGrantFindByIDMapsTokenAndScopes(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_grants" WHERE id = $1`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "scopes", "granular_scopes", "status", "token_kind"}).
			AddRow("g-1", "ws", "ads_read, ads_management", []byte(`{"ads_management":["act_1"]}`), "ACTIVE", "USER"))

	g, err := NewGrantRepository(db).FindByID(context.Background(), "g-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Scopes) != 2 || g.Scopes[1] != "ads_management" || g.GranularScopes["ads_management"][0] != "act_1" ||
		g.Status != advertising.GrantActive || g.TokenKind != facebook.TokenUser {
		t.Fatalf("got %+v", g)
	}
}

func TestGrantFindByIDMissingIsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_grants"`)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := NewGrantRepository(db).FindByID(context.Background(), "g-x"); !errors.Is(err, advertising.ErrGrantNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestGrantMarkCheckedStoresScopes(t *testing.T) {
	db, mock := newMockDB(t)
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectExec(`UPDATE "ad_grants" SET "checked_at"=\$1,"granular_scopes"=\$2,"scopes"=\$3,"updated_at"=\$4 WHERE id = \$5`).
		WithArgs(at, sqlmock.AnyArg(), "ads_read", sqlmock.AnyArg(), "g-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewGrantRepository(db).MarkChecked(context.Background(), "g-1", []string{"ads_read"}, nil, at); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestGrantRevokeOfUnknownGrantIsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(`UPDATE "ad_grants" SET "revoked_at"=\$1,"status"=\$2`).
		WithArgs(sqlmock.AnyArg(), "REVOKED", sqlmock.AnyArg(), "g-x").
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := NewGrantRepository(db).Revoke(context.Background(), "g-x", time.Now()); !errors.Is(err, advertising.ErrGrantNotFound) {
		t.Fatalf("got %v", err)
	}
}
