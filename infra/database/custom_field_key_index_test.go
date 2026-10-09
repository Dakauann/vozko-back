package database

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/infra/database/schema"
)

const legacyCustomFieldKeyIndex = "idx_custom_field_ws_object_key"

func replacingIndex() concurrentIndex {
	return concurrentIndex{
		name:     "idx_new",
		sql:      "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_new ON t (a) WHERE deleted_at IS NULL",
		replaces: "idx_old",
	}
}

func TestAReplacingIndexDropsTheOldOneOnceItIsValid(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`NOT i\.indisvalid`).WithArgs("idx_new").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(`CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_new`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`i\.indisvalid FROM pg_index i WHERE i\.indexrelid = to_regclass\(\$1::text\)`).WithArgs("idx_new").
		WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(true))
	mock.ExpectExec(`DROP INDEX CONCURRENTLY IF EXISTS idx_old`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := createIndexConcurrently(db, replacingIndex()); err != nil {
		t.Fatalf("createIndexConcurrently() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAReplacingIndexKeepsTheOldOneWhileTheNewOneIsNotValid(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`NOT i\.indisvalid`).WithArgs("idx_new").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(`CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_new`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`i\.indisvalid FROM pg_index`).WithArgs("idx_new").
		WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(false))

	err := createIndexConcurrently(db, replacingIndex())
	if !errors.Is(err, errReplacementNotValid) {
		t.Fatalf("createIndexConcurrently() error = %v, want errReplacementNotValid", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAReplacingIndexKeepsTheOldOneWhenValidityIsUnknown(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`NOT i\.indisvalid`).WithArgs("idx_new").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(`CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_new`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`i\.indisvalid FROM pg_index`).WillReturnError(errors.New("catalog unavailable"))

	if err := createIndexConcurrently(db, replacingIndex()); err == nil {
		t.Fatal("createIndexConcurrently() dropped or ignored the old index without knowing the new one is valid")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAReplacingIndexKeepsTheOldOneWhenTheBuildFails(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`NOT i\.indisvalid`).WithArgs("idx_new").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(`CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_new`).
		WillReturnError(errors.New("could not create unique index"))

	if err := createIndexConcurrently(db, replacingIndex()); err == nil {
		t.Fatal("createIndexConcurrently() hid a failed build")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCustomFieldKeysAreUniqueOnlyAmongLiveDefinitions(t *testing.T) {
	for _, idx := range concurrentIndexes() {
		if idx.name != schema.CustomFieldLiveKeyIndex {
			continue
		}
		sql := strings.Join(strings.Fields(idx.sql), " ")
		want := "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS " + schema.CustomFieldLiveKeyIndex +
			" ON custom_field_definitions (workspace_id, object_type, key) WHERE deleted_at IS NULL"
		if sql != want {
			t.Fatalf("sql = %q, want %q", sql, want)
		}
		if idx.replaces != legacyCustomFieldKeyIndex {
			t.Fatalf("replaces = %q, want the legacy non partial index dropped", idx.replaces)
		}
		return
	}
	t.Fatalf("%s is not built", schema.CustomFieldLiveKeyIndex)
}

func TestTheCustomFieldSchemaNoLongerDeclaresTheLegacyIndex(t *testing.T) {
	typ := reflect.TypeOf(schema.CustomFieldDefinition{})
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("gorm")
		if strings.Contains(tag, "uniqueIndex") || strings.Contains(tag, legacyCustomFieldKeyIndex) {
			t.Fatalf("field %s still declares %q; AutoMigrate would rebuild the non partial index", typ.Field(i).Name, tag)
		}
	}
}
