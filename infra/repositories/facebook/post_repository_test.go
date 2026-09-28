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

func TestPostUpsertNeverForgetsThatWePublishedIt(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(`INSERT INTO "facebook_posts" .* ON CONFLICT \("fb_post_id"\) DO UPDATE SET .*"created_by_app"=facebook_posts.created_by_app OR excluded.created_by_app`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := NewPostRepository(db).UpsertMany(context.Background(), []*fbdomain.Post{{WorkspaceID: "ws", PageID: "p", FBPostID: "1_2", Kind: fbdomain.PostStatus}})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostUpsertOfNothingTouchesNothing(t *testing.T) {
	db, _, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	if err := NewPostRepository(db).UpsertMany(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestTrackKeepsTheMirroredFieldsOfAKnownPost(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(`INSERT INTO "facebook_posts" .* ON CONFLICT \("fb_post_id"\) DO UPDATE SET "created_by_app"=facebook_posts.created_by_app OR excluded.created_by_app,"deleted_at"=\$21`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewPostRepository(db).Track(context.Background(), &fbdomain.Post{WorkspaceID: "ws", PageID: "p", FBPostID: "1_2", CreatedByApp: true}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindingAnUnknownPost(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "facebook_posts" WHERE fb_post_id = $1`)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := NewPostRepository(db).FindByFBPostID(context.Background(), "1_2"); !errors.Is(err, fbdomain.ErrPostNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestClaimIsACompareAndSwap(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(`UPDATE "facebook_publish_jobs" SET "attempts"=attempts \+ 1,"status"=\$1,"updated_at"=\$2 WHERE id = \$3 AND status = \$4`).
		WithArgs("UPLOADING", sqlmock.AnyArg(), "job-1", "QUEUED").
		WillReturnResult(sqlmock.NewResult(0, 0))

	claimed, err := NewPublishJobRepository(db).Claim(context.Background(), "job-1", fbdomain.JobQueued, fbdomain.JobUploading)
	if err != nil || claimed {
		t.Fatalf("claimed %v, %v", claimed, err)
	}
}

func TestReleaseStaleReturnsQueuedAndAbandonedJobs(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	cutoff := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`UPDATE facebook_publish_jobs SET status = 'QUEUED', updated_at = NOW\(\) WHERE id IN \(SELECT id FROM facebook_publish_jobs WHERE status IN \('QUEUED','UPLOADING'\) AND updated_at < \$1 ORDER BY updated_at LIMIT \$2 FOR UPDATE SKIP LOCKED\) RETURNING id`).
		WithArgs(cutoff, 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("job-1").AddRow("job-2"))

	ids, err := NewPublishJobRepository(db).ReleaseStale(context.Background(), cutoff, 10)
	if err != nil || len(ids) != 2 {
		t.Fatalf("ids %v, %v", ids, err)
	}
}

func TestJobRoundTripsItsRequestAndProgress(t *testing.T) {
	at := time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)
	job := &fbdomain.PublishJob{
		ID: "job-1", PageID: "p", Status: fbdomain.JobUploading,
		Request:  fbdomain.PublishRequest{Kind: fbdomain.PublishAlbum, Message: "oi", ScheduledAt: &at, Media: []fbdomain.MediaRef{{URL: "https://r2/a.jpg"}}},
		Progress: fbdomain.JobProgress{PhotoIDs: []string{"ph1"}},
	}
	record, err := toJobRecord(job)
	if err != nil {
		t.Fatal(err)
	}
	back, err := toJobDomain(record)
	if err != nil {
		t.Fatal(err)
	}
	if back.Request.Kind != fbdomain.PublishAlbum || !back.Request.ScheduledAt.Equal(at) || len(back.Progress.PhotoIDs) != 1 || record.Kind != "album" {
		t.Fatalf("round trip = %+v", back)
	}
}

func TestAppMadeAmongReadsOnlyOurPosts(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(`SELECT "fb_post_id" FROM "facebook_posts" WHERE \(fb_post_id IN \(\$1,\$2\) AND created_by_app\)`).
		WithArgs("1_1", "1_2").
		WillReturnRows(sqlmock.NewRows([]string{"fb_post_id"}).AddRow("1_2"))
	got, err := NewPostRepository(db).AppMadeAmong(context.Background(), []string{"1_1", "1_2"})
	if err != nil || got["1_1"] || !got["1_2"] {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestProcessingJobIsFoundByItsVideo(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(`SELECT \* FROM "facebook_publish_jobs" WHERE fb_object_id = \$1 AND status = \$2`).
		WithArgs("V1", "PROCESSING", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "request", "status"}).AddRow("job-1", `{"kind":"video"}`, "PROCESSING"))
	job, err := NewPublishJobRepository(db).FindProcessingByVideoID(context.Background(), "V1")
	if err != nil || job.ID != "job-1" {
		t.Fatalf("job %+v %v", job, err)
	}
}

func TestDueProcessingJobsAreThoseWhoseCheckTimePassed(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM "facebook_publish_jobs" WHERE status = \$1 AND next_check_at <= \$2 ORDER BY next_check_at LIMIT \$3`).
		WithArgs("PROCESSING", now, 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "request"}))
	if _, err := NewPublishJobRepository(db).ListDueProcessing(context.Background(), now, 20); err != nil {
		t.Fatal(err)
	}
}

func TestReelsAreCountedPerPageAndDay(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	since := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "facebook_publish_jobs" WHERE page_id = \$1 AND kind = \$2 AND created_at >= \$3 AND status <> \$4`).
		WithArgs("p", "reel", since, "FAILED").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
	n, err := NewPublishJobRepository(db).CountSince(context.Background(), "p", fbdomain.PublishReel, since)
	if err != nil || n != 7 {
		t.Fatalf("n %d %v", n, err)
	}
}
