package advertising_repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/advertising"
)

const accountUpsertPattern = `INSERT INTO ad_accounts .* ON CONFLICT \(meta_account_id\) DO UPDATE SET .*` +
	`business_id = EXCLUDED\.business_id, .*WHERE ad_accounts\.workspace_id = EXCLUDED\.workspace_id RETURNING id, created_at, updated_at`

func connectedAccount() *advertising.AdAccount {
	return &advertising.AdAccount{
		WorkspaceID:   "ws",
		GrantID:       "g-1",
		MetaAccountID: "act_123",
		Name:          "Loja",
		BusinessID:    "bm-1",
		Currency:      "BRL",
		Timezone:      "America/Sao_Paulo",
		MetaStatus:    advertising.MetaAccountActive,
		HasFunding:    true,
		Connection:    advertising.ConnectionConnected,
	}
}

func TestAccountUpsertInsertsOrRefreshesWithinTheSameWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	now := time.Now()
	mock.ExpectQuery(accountUpsertPattern).
		WithArgs(sqlmock.AnyArg(), "ws", "g-1", "123", "Loja", "bm-1", "", "BRL", "America/Sao_Paulo", 1, 0, true, int64(0), int64(0), "CONNECTED").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow("a-1", now, now))

	a := connectedAccount()
	if err := NewAccountRepository(db).Upsert(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if a.ID != "a-1" || a.MetaAccountID != "123" {
		t.Fatalf("got %+v", a)
	}
	expectationsMet(t, mock)
}

func TestAccountUpsertOwnedByAnotherWorkspaceIsLinkedElsewhere(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(accountUpsertPattern).WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}))

	a := connectedAccount()
	if err := NewAccountRepository(db).Upsert(context.Background(), a); !errors.Is(err, advertising.ErrAccountLinkedElsewhere) {
		t.Fatalf("got %v", err)
	}
	if a.ID != "" {
		t.Fatalf("id leaked: %q", a.ID)
	}
}

func TestAccountUpsertWithoutWorkspaceRunsNoQuery(t *testing.T) {
	db, mock := newMockDB(t)
	a := connectedAccount()
	a.WorkspaceID = " "
	if err := NewAccountRepository(db).Upsert(context.Background(), a); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestAccountFindByIDIsScopedToTheWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_accounts" WHERE workspace_id = $1 AND id = $2`)).
		WithArgs("ws", "a-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := NewAccountRepository(db).FindByID(context.Background(), "ws", "a-1"); !errors.Is(err, advertising.ErrAccountNotFound) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestAccountFindByIDMapsTheRow(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_accounts"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "meta_account_id", "business_id", "meta_status", "has_funding", "connection", "amount_spent"}).
			AddRow("a-1", "ws", "123", "bm-1", 9, true, "NEEDS_RECONNECT", int64(500)))
	a, err := NewAccountRepository(db).FindByID(context.Background(), "ws", "a-1")
	if err != nil {
		t.Fatal(err)
	}
	if a.MetaStatus != advertising.MetaAccountInGracePeriod || a.Connection != advertising.ConnectionNeedsReconnect ||
		!a.HasFunding || a.AmountSpent != 500 || a.MetaAccountID != "123" || a.BusinessID != "bm-1" {
		t.Fatalf("got %+v", a)
	}
}

func TestAccountListByWorkspaceHidesDisconnected(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_accounts" WHERE workspace_id = $1 AND connection <> $2 ORDER BY name, id`)).
		WithArgs("ws", "DISCONNECTED").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("a-1").AddRow("a-2"))
	accounts, err := NewAccountRepository(db).ListByWorkspace(context.Background(), "ws")
	if err != nil || len(accounts) != 2 {
		t.Fatalf("got %v, %v", accounts, err)
	}
}

func TestAccountListByWorkspaceRequiresWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	if _, err := NewAccountRepository(db).ListByWorkspace(context.Background(), ""); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestAccountListConnectedPagesById(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_accounts" WHERE connection = $1 ORDER BY id LIMIT $2 OFFSET $3`)).
		WithArgs("CONNECTED", 50, 100).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("a-1"))
	accounts, err := NewAccountRepository(db).ListConnected(context.Background(), 50, 100)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("got %v, %v", accounts, err)
	}
}

func TestAccountSetConnectionOfUnknownAccountIsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(`UPDATE "ad_accounts" SET "connection"=\$1,"updated_at"=\$2 WHERE id = \$3`).
		WithArgs("DISCONNECTED", sqlmock.AnyArg(), "a-x").
		WillReturnResult(sqlmock.NewResult(0, 0))
	err := NewAccountRepository(db).SetConnection(context.Background(), "a-x", advertising.ConnectionDisconnected)
	if !errors.Is(err, advertising.ErrAccountNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestAccountMarkSyncedStampsTheTime(t *testing.T) {
	db, mock := newMockDB(t)
	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	mock.ExpectExec(`UPDATE "ad_accounts" SET "last_synced_at"=\$1,"updated_at"=\$2 WHERE id = \$3`).
		WithArgs(at, sqlmock.AnyArg(), "a-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewAccountRepository(db).MarkSynced(context.Background(), "a-1", at); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestAccountFindByMetaAccountIDNormalizesAndHidesDisconnected(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_accounts" WHERE meta_account_id = $1 AND connection <> $2`)).
		WithArgs("123", "DISCONNECTED", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "meta_account_id"}).AddRow("a-1", "ws", "123"))
	a, err := NewAccountRepository(db).FindByMetaAccountID(context.Background(), " act_123 ")
	if err != nil || a.ID != "a-1" || a.WorkspaceID != "ws" {
		t.Fatalf("got %+v, %v", a, err)
	}
	expectationsMet(t, mock)
}

func TestAccountFindByMetaAccountIDMissingIsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "ad_accounts"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := NewAccountRepository(db).FindByMetaAccountID(context.Background(), "act_9"); !errors.Is(err, advertising.ErrAccountNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestAccountFindByMetaAccountIDBlankRunsNoQuery(t *testing.T) {
	db, mock := newMockDB(t)
	if _, err := NewAccountRepository(db).FindByMetaAccountID(context.Background(), "act_"); !errors.Is(err, errMetaAccountRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}
