package mercadolivre_repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	mldomain "vozko/domain/mercadolivre"
)

func TestFindOrCreateContactIsRaceSafe(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectExec(`INSERT INTO "mercadolivre_contacts" .* ON CONFLICT \("account_id","ml_user_id"\) WHERE deleted_at IS NULL DO NOTHING`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "mercadolivre_contacts" WHERE (account_id = $1 AND ml_user_id = $2)`)).
		WithArgs("a-1", "123456", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "account_id", "ml_user_id", "nickname"}).AddRow("ct-1", "a-1", "123456", "COMPRADOR"))

	c, err := NewContactRepository(db).FindOrCreateByUserID(context.Background(), mldomain.FindOrCreateContactInput{
		WorkspaceID: "ws-1", AccountID: "a-1", MLUserID: "123456", Nickname: "COMPRADOR",
	})
	if err != nil || c.ID != "ct-1" || c.Nickname != "COMPRADOR" {
		t.Fatalf("contact = %+v, %v", c, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindOrCreateContactRefusesABadUserID(t *testing.T) {
	db, _, _ := newMockDB(t)
	_, err := NewContactRepository(db).FindOrCreateByUserID(context.Background(), mldomain.FindOrCreateContactInput{
		WorkspaceID: "ws-1", AccountID: "a-1", MLUserID: "abc",
	})
	if !errors.Is(err, mldomain.ErrUserIDInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestFindContactsByIDsIsOneQueryWithoutDuplicates(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "mercadolivre_contacts" WHERE id IN ($1,$2) AND "mercadolivre_contacts"."deleted_at" IS NULL`)).
		WithArgs("ct-1", "ct-2").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("ct-1").AddRow("ct-2"))
	got, err := NewContactRepository(db).FindByIDs(context.Background(), []string{"ct-1", "ct-2", "ct-1"})
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateContactNicknameStampsTheRefresh(t *testing.T) {
	db, mock, _ := newMockDB(t)
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	mock.ExpectExec(`UPDATE "mercadolivre_contacts" SET "nickname"=\$1,"profile_refreshed_at"=\$2`).
		WithArgs("NOVO", at, sqlmock.AnyArg(), "ct-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewContactRepository(db).UpdateNickname(context.Background(), "ct-1", " NOVO ", at); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMissingContactIsNotFound(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "mercadolivre_contacts"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := NewContactRepository(db).FindByID(context.Background(), "ct-9"); !errors.Is(err, mldomain.ErrContactNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestABlockOnAContactIsSetAndClearedByID(t *testing.T) {
	db, mock, _ := newMockDB(t)
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	mock.ExpectExec(`UPDATE "mercadolivre_contacts" SET "questions_blocked_at"=\$1,"updated_at"=\$2 WHERE id = \$3`).
		WithArgs(at, sqlmock.AnyArg(), "ct-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "mercadolivre_contacts" SET "questions_blocked_at"=\$1,"updated_at"=\$2 WHERE id = \$3`).
		WithArgs(nil, sqlmock.AnyArg(), "ct-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "mercadolivre_contacts" SET "questions_blocked_at"`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	repo := NewContactRepository(db)
	if err := repo.SetQuestionsBlocked(context.Background(), "ct-1", &at); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetQuestionsBlocked(context.Background(), "ct-1", nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetQuestionsBlocked(context.Background(), "ct-9", &at); !errors.Is(err, mldomain.ErrContactNotFound) {
		t.Fatalf("missing contact = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheBlacklistMarksOnlyUnblockedUnchangedContactsOfTheAccount(t *testing.T) {
	db, mock, _ := newMockDB(t)
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "mercadolivre_contacts" SET "questions_blocked_at"=$1,"updated_at"=$2 WHERE (account_id = $3 AND ml_user_id IN ($4,$5) AND questions_blocked_at IS NULL AND updated_at < $6) AND "mercadolivre_contacts"."deleted_at" IS NULL`)).
		WithArgs(at, sqlmock.AnyArg(), "a-1", "1", "2", at).
		WillReturnResult(sqlmock.NewResult(0, 1))
	repo := NewContactRepository(db)
	if err := repo.MarkQuestionsBlocked(context.Background(), "a-1", []string{"1", "2", "1"}, at, at); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkQuestionsBlocked(context.Background(), "a-1", nil, at, at); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheBlacklistClearsBlocksItNoLongerListsOnlyIfTheyAreOlderThanTheScan(t *testing.T) {
	db, mock, _ := newMockDB(t)
	before := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "mercadolivre_contacts" SET "questions_blocked_at"=$1,"updated_at"=$2 WHERE (account_id = $3 AND questions_blocked_at < $4) AND NOT (ml_user_id = ANY($5)) AND "mercadolivre_contacts"."deleted_at" IS NULL`)).
		WithArgs(nil, sqlmock.AnyArg(), "a-1", before, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "mercadolivre_contacts" SET "questions_blocked_at"=$1,"updated_at"=$2 WHERE (account_id = $3 AND questions_blocked_at < $4) AND "mercadolivre_contacts"."deleted_at" IS NULL`)).
		WithArgs(nil, sqlmock.AnyArg(), "a-1", before).
		WillReturnResult(sqlmock.NewResult(0, 0))
	repo := NewContactRepository(db)
	if err := repo.ClearQuestionsBlockedExcept(context.Background(), "a-1", []string{"1", "2"}, before); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClearQuestionsBlockedExcept(context.Background(), "a-1", nil, before); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
