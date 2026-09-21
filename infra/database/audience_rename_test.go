package database

import (
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAudienceRename_AlreadyMigratedSkipsTheExpensiveWork(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`to_regclass`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`FROM workspace_member_permissions`).
		WillReturnRows(sqlmock.NewRows([]string{"member_id", "action"}))
	mock.ExpectExec(`DELETE FROM workspace_member_permissions`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery(`to_regclass`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	if err := renameCommentAnalysisToAudience(db); err != nil {
		t.Fatalf("an already-migrated database must migrate cleanly: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAudienceRename_FoldsAnalysisGrantsRatherThanDroppingThem(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`to_regclass`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`FROM workspace_member_permissions`).
		WillReturnRows(sqlmock.NewRows([]string{"member_id", "action"}).
			AddRow("member-1", "read"))
	mock.ExpectExec(`INSERT INTO workspace_member_permissions`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`DELETE FROM workspace_member_permissions`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := foldAnalysisPermissionIntoAudience(db); err != nil {
		t.Fatalf("folding the analysis grant: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAudienceRename_MissingPermissionsTableIsANoOp(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`to_regclass`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	if err := foldAnalysisPermissionIntoAudience(db); err != nil {
		t.Fatalf("a database without the table must be a no-op: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAudienceRename_NeverDependsOnGenRandomUUID(t *testing.T) {
	for name, stmt := range map[string]string{
		"analysisGrantSelectSQL": analysisGrantSelectSQL,
		"analysisGrantInsertSQL": analysisGrantInsertSQL,
	} {
		if strings.Contains(strings.ToLower(stmt), "gen_random_uuid") {
			t.Errorf("%s must not depend on gen_random_uuid()", name)
		}
	}
}
