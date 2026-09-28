package facebook_repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	fbdomain "vozko/domain/facebook"
)

func TestCommentUpsertKeepsTheLinkedContactAndTombstone(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(`INSERT INTO "facebook_comments" .* ON CONFLICT \("fb_comment_id"\) DO UPDATE SET "from_name"="excluded"."from_name","message"="excluded"."message","like_count"="excluded"."like_count","reply_count"="excluded"."reply_count","is_hidden"="excluded"."is_hidden","liked_by_page"="excluded"."liked_by_page","updated_at"="excluded"."updated_at"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	err := NewCommentRepository(db).UpsertMany(context.Background(), []*fbdomain.Comment{{WorkspaceID: "ws", PageID: "p", FBCommentID: "1_c", FBPostID: "1_2"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLikesNeverGoBelowZero(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(`UPDATE "facebook_comments" SET "like_count"=GREATEST\(like_count \+ \$1, 0\)`).
		WithArgs(-1, sqlmock.AnyArg(), "1_c").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewCommentRepository(db).AddLikes(context.Background(), "1_c", -1); err != nil {
		t.Fatal(err)
	}
}

func TestPostCountsMoveTogether(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(`UPDATE "facebook_posts" SET "comments_count"=GREATEST\(comments_count \+ \$1, 0\),"reactions_count"=GREATEST\(reactions_count \+ \$2, 0\)`).
		WithArgs(1, 0, sqlmock.AnyArg(), "1_2").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewPostRepository(db).AddCounts(context.Background(), "1_2", 0, 1); err != nil {
		t.Fatal(err)
	}
}

func TestRemovedCommentIsATombstone(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	mock.ExpectExec(`UPDATE "facebook_comments" SET "removed_at"=\$1,"updated_at"=\$2 WHERE fb_comment_id = \$3`).
		WithArgs(at, sqlmock.AnyArg(), "1_c").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewCommentRepository(db).MarkRemoved(context.Background(), "1_c", at); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownCommentIsNotFound(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "facebook_comments" WHERE fb_comment_id = $1`)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := NewCommentRepository(db).FindByFBCommentID(context.Background(), "x"); !errors.Is(err, fbdomain.ErrCommentNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestCommentContactsAreReadInOneQuery(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(`SELECT fb_comment_id, contact_id FROM "facebook_comments" WHERE \(fb_comment_id IN \(\$1,\$2\) AND contact_id IS NOT NULL\)`).
		WithArgs("1_a", "1_b").
		WillReturnRows(sqlmock.NewRows([]string{"fb_comment_id", "contact_id"}).AddRow("1_a", "c-1"))
	got, err := NewCommentRepository(db).ContactsFor(context.Background(), []string{"1_a", "1_b"})
	if err != nil || len(got) != 1 || got["1_a"] != "c-1" {
		t.Fatalf("got %v %v", got, err)
	}
}
