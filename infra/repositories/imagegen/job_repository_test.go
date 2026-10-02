package imagegen_repository

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/imagegen"
)

const (
	jobID       = "5b0c6f1e-8d7a-4c2b-9e3f-1a2b3c4d5e6f"
	workspaceID = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"
	requesterID = "1f2e3d4c-5b6a-4978-8a6b-5c4d3e2f1a0b"
)

var at = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		sqlDB.Close()
	})
	return db, mock, sqlDB
}

var jobColumns = []string{"id", "workspace_id", "requested_by", "fingerprint", "prompt", "aspect", "reference_media_ids", "status", "media_id", "media_url", "model", "failure_code", "attempts", "created_at", "updated_at", "finished_at"}

func jobRow(status, failure string) *sqlmock.Rows {
	return referencedJobRow(status, failure, "{}")
}

func referencedJobRow(status, failure, references string) *sqlmock.Rows {
	return sqlmock.NewRows(jobColumns).AddRow(jobID, workspaceID, requesterID, "fp", "pizza", "square", references, status, "", "", "", failure, 1, at, at, nil)
}

func TestClaimMovesOnlyAQueuedJobToRunning(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectQuery(`UPDATE image_generation_jobs SET status = \$1, attempts = attempts \+ 1, updated_at = NOW\(\) WHERE id = \$2 AND status = \$3 RETURNING \*`).
		WithArgs("running", jobID, "queued").
		WillReturnRows(jobRow("running", ""))
	job, claimed, err := NewJobRepository(db).Claim(context.Background(), jobID)
	if err != nil || !claimed || job.Status != imagegen.StatusRunning || job.WorkspaceID != workspaceID || job.Attempts != 1 {
		t.Fatalf("job %+v claimed %v err %v", job, claimed, err)
	}
}

func TestAJobAlreadyClaimedIsNotClaimedAgain(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectQuery(`UPDATE image_generation_jobs SET status = \$1`).
		WithArgs("running", jobID, "queued").
		WillReturnRows(sqlmock.NewRows(jobColumns))
	job, claimed, err := NewJobRepository(db).Claim(context.Background(), jobID)
	if err != nil || claimed || job != nil {
		t.Fatalf("job %+v claimed %v err %v", job, claimed, err)
	}
}

func TestAMalformedIDIsNeverSentToTheDatabase(t *testing.T) {
	db, _, _ := newMockDB(t)
	repo := NewJobRepository(db)
	if _, _, err := repo.Claim(context.Background(), "nope"); !errors.Is(err, imagegen.ErrJobNotFound) {
		t.Fatalf("claim: %v", err)
	}
	if _, err := repo.Get(context.Background(), workspaceID, "nope"); !errors.Is(err, imagegen.ErrJobNotFound) {
		t.Fatalf("get: %v", err)
	}
}

func TestGetIsScopedToTheWorkspace(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "image_generation_jobs" WHERE id = \$1 AND workspace_id = \$2 ORDER BY "image_generation_jobs"."id" LIMIT \$3`).
		WithArgs(jobID, workspaceID, 1).
		WillReturnRows(sqlmock.NewRows(jobColumns))
	if _, err := NewJobRepository(db).Get(context.Background(), workspaceID, jobID); !errors.Is(err, imagegen.ErrJobNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestAStoredJobWithAnUnknownStatusIsRefused(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "image_generation_jobs"`).WillReturnRows(jobRow("paused", ""))
	if _, err := NewJobRepository(db).Get(context.Background(), workspaceID, jobID); !errors.Is(err, imagegen.ErrUnknownJobStatus) {
		t.Fatalf("got %v", err)
	}
}

func TestAStoredJobWithAnUnknownFailureIsRefused(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "image_generation_jobs"`).WillReturnRows(jobRow("failed", "oops"))
	if _, err := NewJobRepository(db).Get(context.Background(), workspaceID, jobID); !errors.Is(err, imagegen.ErrUnknownFailureCode) {
		t.Fatalf("got %v", err)
	}
}

func TestFindActiveLooksOnlyAtUnfinishedRecentJobsOfTheRequester(t *testing.T) {
	db, mock, _ := newMockDB(t)
	since := at.Add(-10 * time.Minute)
	mock.ExpectQuery(`SELECT \* FROM "image_generation_jobs" WHERE workspace_id = \$1 AND requested_by = \$2 AND fingerprint = \$3 AND status IN \(\$4,\$5\) AND created_at >= \$6 ORDER BY created_at DESC,"image_generation_jobs"."id" LIMIT \$7`).
		WithArgs(workspaceID, requesterID, "fp", "queued", "running", since, 1).
		WillReturnRows(jobRow("queued", ""))
	job, err := NewJobRepository(db).FindActive(context.Background(), workspaceID, requesterID, "fp", since)
	if err != nil || job.ID != jobID {
		t.Fatalf("job %+v err %v", job, err)
	}
}

func TestFindActiveWithoutAMatchIsNotFound(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "image_generation_jobs"`).WillReturnRows(sqlmock.NewRows(jobColumns))
	if _, err := NewJobRepository(db).FindActive(context.Background(), workspaceID, requesterID, "fp", at); !errors.Is(err, imagegen.ErrJobNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestASecondActiveJobForTheSameImageIsADuplicate(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectExec(`INSERT INTO "image_generation_jobs"`).
		WillReturnError(errors.New(`pq: duplicate key value violates unique constraint "idx_image_generation_jobs_active"`))
	job := &imagegen.Job{WorkspaceID: workspaceID, RequestedBy: requesterID, Prompt: "pizza", Aspect: imagegen.AspectSquare, Fingerprint: "fp", Status: imagegen.StatusQueued}
	if err := NewJobRepository(db).Create(context.Background(), job); !errors.Is(err, imagegen.ErrDuplicateActiveJob) {
		t.Fatalf("got %v", err)
	}
}

func TestCreateFillsTheGeneratedID(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectExec(`INSERT INTO "image_generation_jobs"`).WillReturnResult(sqlmock.NewResult(0, 1))
	job := &imagegen.Job{WorkspaceID: workspaceID, RequestedBy: requesterID, Prompt: "pizza", Aspect: imagegen.AspectSquare, Fingerprint: "fp", Status: imagegen.StatusQueued}
	if err := NewJobRepository(db).Create(context.Background(), job); err != nil || job.ID == "" || job.CreatedAt.IsZero() {
		t.Fatalf("job %+v err %v", job, err)
	}
}

func TestMarkDoneOnlyFinishesARunningJob(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectExec(`UPDATE "image_generation_jobs" SET "finished_at"=\$1,"media_id"=\$2,"media_url"=\$3,"model"=\$4,"status"=\$5,"updated_at"=\$6 WHERE id = \$7 AND status = \$8`).
		WithArgs(at, "m-1", "https://cdn/x.jpg", "model-x", "done", sqlmock.AnyArg(), jobID, "running").
		WillReturnResult(sqlmock.NewResult(0, 0))
	err := NewJobRepository(db).MarkDone(context.Background(), jobID, imagegen.Result{MediaID: "m-1", MediaURL: "https://cdn/x.jpg", Model: "model-x"}, at)
	if !errors.Is(err, imagegen.ErrJobNotActive) {
		t.Fatalf("got %v", err)
	}
}

func TestMarkFailedOnlyTouchesAnUnfinishedJob(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectExec(`UPDATE "image_generation_jobs" SET "failure_code"=\$1,"finished_at"=\$2,"status"=\$3,"updated_at"=\$4 WHERE id = \$5 AND status IN \(\$6,\$7\)`).
		WithArgs("storage_failed", at, "failed", sqlmock.AnyArg(), jobID, "queued", "running").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewJobRepository(db).MarkFailed(context.Background(), jobID, imagegen.FailureStorage, at); err != nil {
		t.Fatal(err)
	}
}

func TestMarkFailedRefusesAnUnknownCode(t *testing.T) {
	db, _, _ := newMockDB(t)
	if err := NewJobRepository(db).MarkFailed(context.Background(), jobID, "oops", at); !errors.Is(err, imagegen.ErrUnknownFailureCode) {
		t.Fatalf("got %v", err)
	}
}

func TestFailStaleTimesOutOnlyUnfinishedJobs(t *testing.T) {
	db, mock, _ := newMockDB(t)
	cutoff := at.Add(-10 * time.Minute)
	mock.ExpectQuery(`UPDATE image_generation_jobs SET status = \$1, failure_code = \$2, finished_at = NOW\(\), updated_at = NOW\(\) WHERE id IN \(SELECT id FROM image_generation_jobs WHERE status IN \(\$3, \$4\) AND created_at < \$5 ORDER BY created_at LIMIT \$6 FOR UPDATE SKIP LOCKED\) AND status IN \(\$7, \$8\) RETURNING id`).
		WithArgs("failed", "timed_out", "queued", "running", cutoff, 100, "queued", "running").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("a").AddRow("b"))
	ids, err := NewJobRepository(db).FailStale(context.Background(), cutoff, 100)
	if err != nil || len(ids) != 2 {
		t.Fatalf("ids %v err %v", ids, err)
	}
}

func TestRecordRoundTrip(t *testing.T) {
	finished := at.Add(time.Minute)
	job := &imagegen.Job{ID: jobID, WorkspaceID: workspaceID, RequestedBy: requesterID, Prompt: "pizza", Aspect: imagegen.AspectStory,
		Fingerprint: "fp", Status: imagegen.StatusDone, MediaID: "m-1", MediaURL: "https://cdn/x.jpg", Model: "model-x", Attempts: 1, FinishedAt: &finished}
	back, err := toDomain(toRecord(job))
	if err != nil {
		t.Fatal(err)
	}
	if *back.FinishedAt != finished || back.Aspect != imagegen.AspectStory || back.MediaURL != job.MediaURL || back.Status != imagegen.StatusDone {
		t.Fatalf("round trip %+v", back)
	}
}

func TestAClaimedJobCarriesItsReferencesInOrder(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectQuery(`UPDATE image_generation_jobs SET status = \$1`).
		WithArgs("running", jobID, "queued").
		WillReturnRows(referencedJobRow("running", "", "{m-2,m-1}"))
	job, claimed, err := NewJobRepository(db).Claim(context.Background(), jobID)
	if err != nil || !claimed || !reflect.DeepEqual(job.ReferenceMediaIDs, []string{"m-2", "m-1"}) {
		t.Fatalf("job %+v claimed %v err %v", job, claimed, err)
	}
}

func TestReferencesSurviveTheRecordRoundTrip(t *testing.T) {
	job := &imagegen.Job{ID: jobID, WorkspaceID: workspaceID, RequestedBy: requesterID, Prompt: "pizza", Aspect: imagegen.AspectSquare,
		ReferenceMediaIDs: []string{"m-2", "m-1"}, Fingerprint: "fp", Status: imagegen.StatusQueued}
	back, err := toDomain(toRecord(job))
	if err != nil || !reflect.DeepEqual(back.ReferenceMediaIDs, job.ReferenceMediaIDs) {
		t.Fatalf("round trip %+v err %v", back, err)
	}
	job.ReferenceMediaIDs = nil
	record := toRecord(job)
	if record.ReferenceMediaIDs == nil || len(record.ReferenceMediaIDs) != 0 {
		t.Fatalf("a job without references must store an empty list, got %#v", record.ReferenceMediaIDs)
	}
	if back, _ := toDomain(record); len(back.ReferenceMediaIDs) != 0 {
		t.Fatalf("round trip %+v", back)
	}
}
