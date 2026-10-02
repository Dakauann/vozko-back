package media_repository

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/media"
)

const knownMediaID = "5b0c6f1e-8d7a-4c2b-9e3f-1a2b3c4d5e6f"

func newMockRepository(t *testing.T) (*MediaRepository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		sqlDB.Close()
	})
	return NewMediaRepository(db), mock
}

func TestAMissingMediaIsNotFound(t *testing.T) {
	repo, mock := newMockRepository(t)
	mock.ExpectQuery(`SELECT \* FROM "medias"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := repo.GetMediaByID(knownMediaID); !errors.Is(err, media.ErrMediaNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestAMalformedMediaIDIsNotFoundWithoutQuerying(t *testing.T) {
	repo, _ := newMockRepository(t)
	if _, err := repo.GetMediaByID("not-a-uuid"); !errors.Is(err, media.ErrMediaNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestADatabaseFailureIsNotMistakenForAMissingMedia(t *testing.T) {
	repo, mock := newMockRepository(t)
	mock.ExpectQuery(`SELECT \* FROM "medias"`).WillReturnError(errors.New("connection reset"))
	_, err := repo.GetMediaByID(knownMediaID)
	if err == nil || errors.Is(err, media.ErrMediaNotFound) {
		t.Fatalf("got %v", err)
	}
}
