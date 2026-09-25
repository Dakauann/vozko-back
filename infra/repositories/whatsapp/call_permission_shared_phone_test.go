package whatsapp_repository

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	callpermission "vozko/domain/whatsapp/call_permission"
)

func mockedRepo(t *testing.T) (*callPermissionRepository, sqlmock.Sqlmock) {
	t.Helper()
	conn, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm open: %v", err)
	}
	return &callPermissionRepository{db: db}, mock
}

func TestUpsert_OwnedPhoneInsertsWithOnConflict(t *testing.T) {
	repo, mock := mockedRepo(t)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "whatsapp_call_permissions".*ON CONFLICT`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := repo.Upsert(&callpermission.CallPermission{
		WorkspaceID:     "11111111-1111-1111-1111-111111111111",
		BusinessPhoneID: "22222222-2222-2222-2222-222222222222",
		UserNumber:      "5511976952456",
		Status:          callpermission.StatusGranted,
	}); err != nil {
		t.Fatalf("owned phone upsert must succeed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("%v", err)
	}
}

func TestUpsert_SharedPhoneUpdatesExistingRowAndKeepsWorkspace(t *testing.T) {
	repo, mock := mockedRepo(t)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "whatsapp_call_permissions" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.Upsert(&callpermission.CallPermission{
		BusinessPhoneID: "22222222-2222-2222-2222-222222222222",
		UserNumber:      "5511976952456",
		Status:          callpermission.StatusGranted,
	}); err != nil {
		t.Fatalf("shared phone grant must be recorded: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("%v", err)
	}
}

func TestUpsert_SharedPhoneNeverWritesWorkspaceColumn(t *testing.T) {
	var executed []string

	conn, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(
		sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
			executed = append(executed, actualSQL)
			return sqlmock.QueryMatcherRegexp.Match(expectedSQL, actualSQL)
		}),
	))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer conn.Close()

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm open: %v", err)
	}
	repo := &callPermissionRepository{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "whatsapp_call_permissions" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.Upsert(&callpermission.CallPermission{
		BusinessPhoneID: "22222222-2222-2222-2222-222222222222",
		UserNumber:      "5511976952456",
		Status:          callpermission.StatusGranted,
	}); err != nil {
		t.Fatalf("shared phone grant must be recorded: %v", err)
	}

	if len(executed) == 0 {
		t.Fatal("no statement was captured")
	}
	workspaceCol := regexp.MustCompile(`(?i)set[^;]*workspace_id`)
	for _, q := range executed {
		if workspaceCol.MatchString(q) {
			t.Fatalf("shared-phone update must not reassign workspace_id: %s", q)
		}
		if !regexp.MustCompile(`(?i)^\s*update`).MatchString(q) {
			t.Fatalf("expected an UPDATE, got: %s", q)
		}
	}
}

func TestUpsert_SharedPhoneWithNoExistingRowFailsLoudly(t *testing.T) {
	repo, mock := mockedRepo(t)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "whatsapp_call_permissions" SET`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err := repo.Upsert(&callpermission.CallPermission{
		BusinessPhoneID: "22222222-2222-2222-2222-222222222222",
		UserNumber:      "5511976952456",
		Status:          callpermission.StatusGranted,
	})
	if err == nil {
		t.Fatal("a grant with no prior row and no workspace must not be silently dropped")
	}
}

func TestUpsert_RequiresPhoneAndNumber(t *testing.T) {
	repo, _ := mockedRepo(t)

	if err := repo.Upsert(&callpermission.CallPermission{
		WorkspaceID: "11111111-1111-1111-1111-111111111111",
		UserNumber:  "5511976952456",
	}); err == nil {
		t.Fatal("missing business phone id must error")
	}
}
