package workflowsecret_repository

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/workflowsecret"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func expectationsMet(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertSQLBindsEveryPlaceholderOnce(t *testing.T) {
	if got := strings.Count(upsertSecretSQL, "?"); got != 8 {
		t.Fatalf("upsert has %d placeholders, want 8", got)
	}
}

func TestUpsertReplacesTheValueByWorkspaceAndNameKeepingTheCreator(t *testing.T) {
	db, mock := newMockDB(t)
	created := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`INSERT INTO workflow_secrets .* ON CONFLICT \(workspace_id, name\) DO UPDATE SET encrypted_value = EXCLUDED.encrypted_value, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at RETURNING id, created_by, created_at`).
		WithArgs("id-new", "ws", "senha", []byte("cipher"), "u2", "u2", now, now).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_by", "created_at"}).AddRow("id-old", "u1", created))

	got, err := New(db).Upsert(context.Background(), workflowsecret.Secret{
		ID: "id-new", WorkspaceID: "ws", Name: "senha", EncryptedValue: []byte("cipher"),
		CreatedBy: "u2", UpdatedBy: "u2", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "id-old" || got.CreatedBy != "u1" || !got.CreatedAt.Equal(created) || got.UpdatedBy != "u2" {
		t.Fatalf("got %+v", got)
	}
	expectationsMet(t, mock)
}

func TestDeleteRemovesTheRowAndReportsAMissingSecret(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "workflow_secrets" WHERE workspace_id = $1 AND name = $2`)).
		WithArgs("ws", "senha").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "workflow_secrets" WHERE workspace_id = $1 AND name = $2`)).
		WithArgs("ws", "absent").WillReturnResult(sqlmock.NewResult(0, 0))
	repo := New(db)
	if err := repo.Delete(context.Background(), "ws", "senha"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(context.Background(), "ws", "absent"); !errors.Is(err, workflowsecret.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestListNeverReadsTheEncryptedValue(t *testing.T) {
	db, mock := newMockDB(t)
	at := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT "name","updated_at","updated_by" FROM "workflow_secrets" WHERE workspace_id = $1 ORDER BY name`)).
		WithArgs("ws").
		WillReturnRows(sqlmock.NewRows([]string{"name", "updated_at", "updated_by"}).AddRow("api_key", at, "u1"))
	got, err := New(db).List(context.Background(), "ws")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "api_key" || got[0].UpdatedBy != "u1" || !got[0].UpdatedAt.Equal(at) {
		t.Fatalf("got %+v", got)
	}
	expectationsMet(t, mock)
}

func TestFindByNamesIsOneQueryOverTheUniqueIndex(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "workflow_secrets" WHERE workspace_id = $1 AND name IN ($2,$3)`)).
		WithArgs("ws", "login", "senha").
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "name", "encrypted_value"}).AddRow("i1", "ws", "senha", []byte("c")))
	got, err := New(db).FindByNames(context.Background(), "ws", []string{"login", "senha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "senha" || string(got[0].EncryptedValue) != "c" {
		t.Fatalf("got %+v", got)
	}
	expectationsMet(t, mock)
}

func TestEmptyInputsNeverQuery(t *testing.T) {
	db, mock := newMockDB(t)
	repo := New(db)
	if got, err := repo.FindByNames(context.Background(), "ws", nil); err != nil || len(got) != 0 {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := repo.List(context.Background(), " "); !errors.Is(err, workflowsecret.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	if err := repo.Delete(context.Background(), "", "x"); !errors.Is(err, workflowsecret.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}
