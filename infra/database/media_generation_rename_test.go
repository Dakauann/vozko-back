package database

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMediaGenerationRename_RenamesTheTableAndItsIndexesOnce(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`to_regclass`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec(`ALTER TABLE image_generation_jobs RENAME TO media_generation_jobs`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`to_regclass`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec(`ALTER INDEX idx_image_generation_jobs_active RENAME TO idx_media_generation_jobs_active`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`to_regclass`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec(`ALTER INDEX idx_image_generation_jobs_stale RENAME TO idx_media_generation_jobs_stale`).WillReturnResult(sqlmock.NewResult(0, 0))

	if err := renameImageGenerationToMedia(db); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMediaGenerationRename_AlreadyRenamedDoesNothing(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	for i := 0; i < 3; i++ {
		mock.ExpectQuery(`to_regclass`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	}
	if err := renameImageGenerationToMedia(db); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
