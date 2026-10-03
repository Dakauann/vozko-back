package advertising_repository

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/advertising"
)

func queuedJob() *advertising.PublishJob {
	return &advertising.PublishJob{
		WorkspaceID: "ws",
		AdAccountID: "a-1",
		CreatedBy:   "u-1",
		Actor:       advertising.ActorPerson,
		Draft: advertising.AdDraft{
			AdAccountID: "a-1",
			Campaign:    advertising.CampaignDraft{Name: "Promo"},
			AdSet:       advertising.AdSetDraft{Budget: &advertising.Budget{Kind: advertising.BudgetDaily, Amount: 2000}},
			Ads:         []advertising.AdItem{{Name: "A"}, {Name: "B"}},
		},
		Status: advertising.JobQueued,
	}
}

func TestJobCreateAssignsTheID(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "ad_publish_jobs"`)).WillReturnResult(sqlmock.NewResult(0, 1))
	job := queuedJob()
	if err := NewPublishJobRepository(db).Create(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if job.ID == "" || job.CreatedAt.IsZero() {
		t.Fatalf("got %+v", job)
	}
	expectationsMet(t, mock)
}

func TestJobFindIsScopedAndMapsJSON(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_publish_jobs" WHERE workspace_id = $1 AND id = $2`)).
		WithArgs("ws", "j-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "status", "actor", "draft", "progress", "fee", "fee_micros"}).
			AddRow("j-1", "ws", "RUNNING", "assistant", []byte(`{"campaign":{"name":"Promo"}}`), []byte(`{"campaignId":"c-9","creatives":{"0":"cr-1"},"ads":{"1":"ad-2"}}`), "charged", int64(10)))
	job, err := NewPublishJobRepository(db).Find(context.Background(), "ws", "j-1")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != advertising.JobRunning || job.Actor != advertising.ActorAssistant || job.Draft.Campaign.Name != "Promo" ||
		job.Progress.CampaignID != "c-9" || job.Progress.Creatives[0] != "cr-1" || job.Progress.Ads[1] != "ad-2" || job.Fee != advertising.FeeCharged || job.FeeMicros != 10 {
		t.Fatalf("got %+v", job)
	}
}

func TestJobFindMissingIsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "ad_publish_jobs"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := NewPublishJobRepository(db).Find(context.Background(), "ws", "j-x"); !errors.Is(err, advertising.ErrJobNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestJobSaveIsScopedToTheWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(`UPDATE "ad_publish_jobs" SET "draft"=\$1,"error_code"=\$2,"error_message"=\$3,"fee"=\$4,"fee_micros"=\$5,"progress"=\$6,"status"=\$7,"updated_at"=\$8 WHERE id = \$9 AND workspace_id = \$10`).
		WithArgs(sqlmock.AnyArg(), "meta_error", "boom", "charged", int64(10), sqlmock.AnyArg(), "FAILED", sqlmock.AnyArg(), "j-1", "ws").
		WillReturnResult(sqlmock.NewResult(0, 0))
	job := queuedJob()
	job.ID = "j-1"
	job.Fee, job.FeeMicros = advertising.FeeCharged, 10
	job.Fail("meta_error", "boom")
	if err := NewPublishJobRepository(db).Save(context.Background(), job); !errors.Is(err, advertising.ErrJobNotFound) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestJobClaimIsAnAtomicStatusSwap(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ad_publish_jobs" SET "status"=$1,"updated_at"=NOW() WHERE id = $2 AND status = $3`)).
		WithArgs("RUNNING", "j-1", "QUEUED").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "ad_publish_jobs"`).WillReturnResult(sqlmock.NewResult(0, 0))
	repo := NewPublishJobRepository(db)
	won, err := repo.Claim(context.Background(), "j-1", advertising.JobQueued, advertising.JobRunning)
	if err != nil || !won {
		t.Fatalf("got %v, %v", won, err)
	}
	lost, err := repo.Claim(context.Background(), "j-1", advertising.JobQueued, advertising.JobRunning)
	if err != nil || lost {
		t.Fatalf("got %v, %v", lost, err)
	}
	expectationsMet(t, mock)
}

func TestJobListByWorkspaceIsNewestFirst(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_publish_jobs" WHERE workspace_id = $1 ORDER BY created_at DESC, id LIMIT $2`)).
		WithArgs("ws", 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "draft"}).AddRow("j-2", []byte(`{}`)).AddRow("j-1", []byte(`{}`)))
	jobs, err := NewPublishJobRepository(db).ListByWorkspace(context.Background(), "ws", 20)
	if err != nil || len(jobs) != 2 || jobs[0].ID != "j-2" {
		t.Fatalf("got %v, %v", jobs, err)
	}
}

func TestJobListByWorkspaceRequiresWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	if _, err := NewPublishJobRepository(db).ListByWorkspace(context.Background(), "", 20); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestJobListStaleFindsUnfinishedJobsNotTouchedSince(t *testing.T) {
	db, mock := newMockDB(t)
	before := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_publish_jobs" WHERE status IN ($1,$2) AND updated_at < $3 ORDER BY updated_at, id LIMIT $4`)).
		WithArgs("RUNNING", "QUEUED", before, 10).
		WillReturnRows(sqlmock.NewRows([]string{"id", "draft"}).AddRow("j-1", []byte(`{}`)))
	jobs, err := NewPublishJobRepository(db).ListStale(context.Background(), before, 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("got %v, %v", jobs, err)
	}
}

func TestJobDraftAndProgressSurviveTheJSONColumns(t *testing.T) {
	job := queuedJob()
	job.Progress = advertising.Progress{
		Media:      map[string]string{"media-1": "hash-1"},
		ReadyVideo: map[string]bool{"video-1": true},
		CampaignID: "c-9",
		AdSetID:    "s-9",
		Creatives:  map[int]string{0: "cr-0", 1: "cr-1"},
		Ads:        map[int]string{1: "ad-1"},
		InFlight:   "ad:1",
	}
	record, err := toJobRecord(job)
	if err != nil {
		t.Fatal(err)
	}
	back, err := toJob(record)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Draft, job.Draft) || !reflect.DeepEqual(back.Progress, job.Progress) {
		t.Fatalf("got %+v / %+v", back.Draft, back.Progress)
	}
}
