package mercadolivre_repository

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	mldomain "vozko/domain/mercadolivre"
)

func validAccount() *mldomain.Account {
	return &mldomain.Account{
		ID: "a-1", WorkspaceID: "ws-1", MLUserID: "415458330", SiteID: mldomain.SiteMLB, Status: mldomain.StatusConnected,
		AccessToken: "APP_USR-1", RefreshToken: "TG-1",
	}
}

func TestCreatingASellerLinkedElsewhereIsReported(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "mercadolivre_accounts"`)).
		WillReturnError(errors.New(`pq: duplicate key value violates unique constraint "ux_ml_account_live_user"`))
	err := NewAccountRepository(db).Create(context.Background(), validAccount())
	if !errors.Is(err, mldomain.ErrAccountAlreadyLinked) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateRefusesAnInvalidAccount(t *testing.T) {
	db, _, _ := newMockDB(t)
	a := validAccount()
	a.SiteID = "MLA"
	if err := NewAccountRepository(db).Create(context.Background(), a); !errors.Is(err, mldomain.ErrSiteNotSupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateStatementCarriesNoTokenColumn(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectExec(`UPDATE "mercadolivre_accounts" SET "agent_id"=\$1,"automation_disclosure"=\$2,"department_id"=\$3,"enable_agent_responses"=\$4,"enable_workflow"=\$5,"nickname"=\$6,"pipeline_id"=\$7,"workflow_id"=\$8,"updated_at"=\$9 WHERE id = \$10`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewAccountRepository(db).Update(context.Background(), validAccount()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDisconnectWipesTokensAndSchedulesThePurge(t *testing.T) {
	db, mock, _ := newMockDB(t)
	purge := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	mock.ExpectExec(`UPDATE "mercadolivre_accounts" SET "access_expires_at"=\$1,"access_token"=\$2,"deleted_at"=\$3,"purge_after"=\$4,"refresh_token"=\$5,"status"=\$6,"status_reason"=\$7`).
		WithArgs(nil, nil, sqlmock.AnyArg(), purge, nil, "DISCONNECTED", "disconnected by the workspace", sqlmock.AnyArg(), "a-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	err := NewAccountRepository(db).Disconnect(context.Background(), "a-1", mldomain.StatusDisconnected, "disconnected by the workspace", purge)
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDisconnectRefusesALiveStatus(t *testing.T) {
	db, _, _ := newMockDB(t)
	if err := NewAccountRepository(db).Disconnect(context.Background(), "a-1", mldomain.StatusConnected, "", time.Now()); err == nil {
		t.Fatal("disconnecting into CONNECTED must be refused")
	}
}

func TestUpdateStatusOutOfConnectedWipesTheDeadTokens(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectExec(`UPDATE "mercadolivre_accounts" SET "access_expires_at"=\$1,"access_token"=\$2,"refresh_token"=\$3,"status"=\$4,"status_reason"=\$5`).
		WithArgs(nil, nil, nil, "NEEDS_RECONNECT", "invalid_grant", sqlmock.AnyArg(), "a-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewAccountRepository(db).UpdateStatus(context.Background(), "a-1", mldomain.StatusNeedsReconnect, "invalid_grant"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDueListsAreBoundedAndOrdered(t *testing.T) {
	cutoff := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		pattern string
		call    func(r mldomain.AccountRepository) error
	}{
		{"refresh", `status = \$1 AND refresh_token IS NOT NULL AND \(\(access_expires_at IS NULL OR access_expires_at < \$2\)\).* ORDER BY access_expires_at ASC NULLS FIRST,id LIMIT \$3`,
			func(r mldomain.AccountRepository) error {
				_, err := r.ListDueForRefresh(context.Background(), cutoff, 50)
				return err
			}},
		{"reconcile", `status = \$1 AND \(\(last_reconciled_at IS NULL OR last_reconciled_at < \$2\)\).* ORDER BY last_reconciled_at ASC NULLS FIRST,id LIMIT \$3`,
			func(r mldomain.AccountRepository) error {
				_, err := r.ListDueForReconcile(context.Background(), cutoff, 50)
				return err
			}},
		{"health", `status IN \(\$1,\$2,\$3\) AND \(\(last_health_check_at IS NULL OR last_health_check_at < \$4\)\).* ORDER BY last_health_check_at ASC NULLS FIRST,id LIMIT \$5`,
			func(r mldomain.AccountRepository) error {
				_, err := r.ListDueForHealthCheck(context.Background(), cutoff, 50)
				return err
			}},
		{"purge", `\(purge_after IS NOT NULL AND purge_after <= \$1\) AND status IN \(\$2,\$3\) ORDER BY purge_after ASC,id LIMIT \$4`,
			func(r mldomain.AccountRepository) error {
				_, err := r.ListDueForPurge(context.Background(), cutoff, 50)
				return err
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, _ := newMockDB(t)
			mock.ExpectQuery(tc.pattern).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			if err := tc.call(NewAccountRepository(db)); err != nil {
				t.Fatal(err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDueListsCapAnOversizedLimit(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectQuery(`LIMIT \$3`).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), maxDueBatch).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := NewAccountRepository(db).ListDueForReconcile(context.Background(), time.Now(), 1_000_000); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLockForRefreshStoresTheRotatedPairInTheSameTransaction(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "mercadolivre_accounts" WHERE id = \$1 AND "mercadolivre_accounts"."deleted_at" IS NULL LIMIT \$2 FOR UPDATE SKIP LOCKED`).
		WithArgs("a-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "ml_user_id", "site_id", "status"}).AddRow("a-1", "ws-1", "415458330", "MLB", "CONNECTED"))
	mock.ExpectExec(`UPDATE "mercadolivre_accounts" SET "access_expires_at"=\$1,"access_token"=\$2,"granted_scopes"=\$3,"refresh_expires_at"=\$4,"refresh_token"=\$5`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	expires := time.Date(2026, 10, 19, 18, 0, 0, 0, time.UTC)
	acquired, err := NewAccountRepository(db).LockForRefresh(context.Background(), "a-1", func(_ context.Context, a *mldomain.Account) (mldomain.RefreshOutcome, error) {
		if a.ID != "a-1" {
			t.Fatalf("refresh saw %+v", a)
		}
		return mldomain.RefreshOutcome{Tokens: &mldomain.TokenUpdate{
			AccessToken: "APP_USR-2", RefreshToken: "TG-2", AccessExpiresAt: expires, RefreshExpiresAt: expires.Add(time.Hour),
			GrantedScopes: []string{"offline_access", "read", "write"},
		}}, nil
	})
	if err != nil || !acquired {
		t.Fatalf("acquired=%v err=%v", acquired, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLockForRefreshSkipsARowAnotherRefresherHolds(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`FOR UPDATE SKIP LOCKED`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectCommit()
	called := false
	acquired, err := NewAccountRepository(db).LockForRefresh(context.Background(), "a-1", func(context.Context, *mldomain.Account) (mldomain.RefreshOutcome, error) {
		called = true
		return mldomain.RefreshOutcome{}, nil
	})
	if err != nil || acquired || called {
		t.Fatalf("acquired=%v called=%v err=%v", acquired, called, err)
	}
}

func TestLockForRefreshKeepsThePairWhenTheRefreshFails(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`FOR UPDATE SKIP LOCKED`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("a-1"))
	mock.ExpectRollback()
	boom := errors.New("network")
	acquired, err := NewAccountRepository(db).LockForRefresh(context.Background(), "a-1", func(context.Context, *mldomain.Account) (mldomain.RefreshOutcome, error) {
		return mldomain.RefreshOutcome{}, boom
	})
	if !errors.Is(err, boom) || !acquired {
		t.Fatalf("acquired=%v err=%v", acquired, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshOutcomeShapes(t *testing.T) {
	cases := []struct {
		name    string
		outcome mldomain.RefreshOutcome
		keys    []string
		err     bool
	}{
		{"nothing to do", mldomain.RefreshOutcome{}, nil, false},
		{"needs reconnect wipes the dead token", mldomain.RefreshOutcome{Status: mldomain.StatusNeedsReconnect, StatusReason: "invalid_grant"},
			[]string{"access_expires_at", "access_token", "refresh_token", "status", "status_reason"}, false},
		{"unknown status refused", mldomain.RefreshOutcome{Status: "PAUSED"}, nil, true},
		{"empty token pair refused", mldomain.RefreshOutcome{Tokens: &mldomain.TokenUpdate{AccessToken: "x"}}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			updates, err := refreshUpdates(tc.outcome)
			if (err != nil) != tc.err {
				t.Fatalf("err = %v", err)
			}
			if len(updates) != len(tc.keys) {
				t.Fatalf("updates = %v", updates)
			}
			for _, k := range tc.keys {
				if _, ok := updates[k]; !ok {
					t.Errorf("missing %s in %v", k, updates)
				}
			}
		})
	}
}

func TestListAccountsEscapesTheSearch(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "mercadolivre_accounts" WHERE workspace_id = \$1 AND \(\(nickname ILIKE \$2 OR ml_user_id = \$3\)\)`).
		WithArgs("ws-1", `%50\%\_off%`, "50%_off").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`ORDER BY created_at DESC,id`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	result, err := NewAccountRepository(db).ListByWorkspace(context.Background(), mldomain.ListAccountsInput{WorkspaceID: "ws-1", Search: "50%_off"})
	if err != nil || result == nil {
		t.Fatalf("result=%v err=%v", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListAccountsNeedsAWorkspace(t *testing.T) {
	db, _, _ := newMockDB(t)
	if _, err := NewAccountRepository(db).ListByWorkspace(context.Background(), mldomain.ListAccountsInput{}); !errors.Is(err, mldomain.ErrWorkspaceIDRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestTokensAreNeverStoredInClearText(t *testing.T) {
	withPII(t)
	record := toAccountSchema(validAccount())
	raw, err := record.AccessToken.Value()
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := raw.([]byte); !ok || strings.Contains(string(b), "APP_USR-1") {
		t.Fatalf("stored access token = %v", raw)
	}
}
