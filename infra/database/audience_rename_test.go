package database

import (
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// The audience rename runs at boot on every deployment, inside the migration
// transaction, and RunMigrations' caller log.Fatals on error.
//
// So what is worth pinning is not the happy path but the two ways this can hurt
// a live deployment: doing expensive work it does not need to do, and assuming
// a database feature this codebase does not provide.

// An already-migrated database must do NOTHING beyond the cheap permission
// fold.
//
// The value rewrites are idempotent but not free: service_type is not a leading
// index column on balance_transactions, which in production holds 3.7 million
// rows and zero matches, so an unguarded rewrite scans the whole table on every
// boot to find nothing. The old table's absence is the sentinel that stops it.
func TestAudienceRename_AlreadyMigratedSkipsTheExpensiveWork(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	// The permission fold runs first and sits outside the sentinel.
	mock.ExpectQuery(`to_regclass`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`FROM workspace_member_permissions`).
		WillReturnRows(sqlmock.NewRows([]string{"member_id", "action"}))
	mock.ExpectExec(`DELETE FROM workspace_member_permissions`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	// The sentinel: comment_analyses is gone, so nothing below may run.
	mock.ExpectQuery(`to_regclass`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	if err := renameCommentAnalysisToAudience(db); err != nil {
		t.Fatalf("an already-migrated database must migrate cleanly: %v", err)
	}
	// sqlmock fails on any call it was not told to expect, so an UPDATE or an
	// ALTER reaching the database here is itself the failure.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// A member holding only the retired "analysis" grant is moved onto "audience",
// one INSERT per missing grant, and then the dead rows go.
//
// Insert-then-delete rather than UPDATE because a member may hold BOTH and
// (member_id, resource, action) is unique: an UPDATE would collide and abort
// the whole migration transaction.
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

// A database that has never had the permissions table is not a boot failure.
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

// No statement this migration issues may call gen_random_uuid().
//
// It needs PG13+ or pgcrypto, this codebase creates neither, and every other id
// here is generated in Go for exactly that reason (see datarepairs.go). Asserted
// against the SQL itself rather than through a mock, so it holds on every
// branch, including ones a mock-driven test does not walk.
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
