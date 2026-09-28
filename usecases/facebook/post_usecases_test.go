package facebook

import (
	"context"
	"errors"
	"testing"
	"time"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta"
	webhook_usecase "vozko/usecases/webhook"
)

type postFixture struct {
	uc      *PostUseCases
	worker  *PublishJobWorker
	pages   *fakePages
	service *fakePostService
	posts   *fakePosts
	jobs    *fakeJobs
	queue   *fakeQueue
	now     time.Time
}

func newPostFixture() *postFixture {
	f := &postFixture{
		pages: newFakePages(publishingPage()), service: newFakePostService(), posts: newFakePosts(),
		jobs: newFakeJobs(), queue: &fakeQueue{}, now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC),
	}
	deps := PostDeps{Pages: f.pages, Posts: f.posts, Jobs: f.jobs, Service: f.service, Queue: f.queue}
	f.uc = NewPostUseCases(deps)
	f.uc.now = func() time.Time { return f.now }
	f.worker = NewPublishJobWorker(deps)
	return f
}

func (f *postFixture) create(t *testing.T, req fbdomain.PublishRequest) *fbdomain.PublishJob {
	t.Helper()
	job, err := f.uc.Create(context.Background(), "ws-1", "page-1", "user-1", req)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func (f *postFixture) run(t *testing.T, jobID string) (*fbdomain.PublishJob, error) {
	t.Helper()
	err := f.worker.Execute(context.Background(), &PublishJobMessage{JobID: jobID})
	job, findErr := f.jobs.FindByID(context.Background(), jobID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	return job, err
}

func TestCreateQueuesAValidatedJob(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishText, Message: "Novidades!"})

	if job.Status != fbdomain.JobQueued || job.RequestedBy != "user-1" || job.PageID != "page-1" {
		t.Fatalf("job = %+v", job)
	}
	if len(f.queue.enqueued) != 1 || f.queue.enqueued[0] != job.ID {
		t.Fatalf("queue = %v", f.queue.enqueued)
	}
}

func TestCreateRefusesAnInvalidRequestAndAPageThatCannotPublish(t *testing.T) {
	f := newPostFixture()
	if _, err := f.uc.Create(context.Background(), "ws-1", "page-1", "u", fbdomain.PublishRequest{Kind: fbdomain.PublishText}); !errors.Is(err, fbdomain.ErrInvalidPost) {
		t.Fatalf("invalid: %v", err)
	}
	f.pages.byID["page-1"].Tasks = []fbdomain.Task{fbdomain.TaskMessaging}
	if _, err := f.uc.Create(context.Background(), "ws-1", "page-1", "u", fbdomain.PublishRequest{Kind: fbdomain.PublishText, Message: "x"}); !errors.Is(err, fbdomain.ErrCapabilityDenied) {
		t.Fatalf("capability: %v", err)
	}
	if _, err := f.uc.Create(context.Background(), "ws-other", "page-1", "u", fbdomain.PublishRequest{Kind: fbdomain.PublishText, Message: "x"}); !errors.Is(err, fbdomain.ErrPageNotFound) {
		t.Fatalf("workspace: %v", err)
	}
	if len(f.queue.enqueued) != 0 {
		t.Fatal("nothing may be queued")
	}
}

func TestAJobThatCannotBeQueuedFailsInsteadOfWaitingForever(t *testing.T) {
	f := newPostFixture()
	f.queue.err = errors.New("broker down")

	if _, err := f.uc.Create(context.Background(), "ws-1", "page-1", "u", fbdomain.PublishRequest{Kind: fbdomain.PublishText, Message: "x"}); err == nil {
		t.Fatal("expected the queue failure")
	}
	for _, j := range f.jobs.byID {
		if j.Status != fbdomain.JobFailed {
			t.Fatalf("job left %s", j.Status)
		}
	}
}

func TestWorkerPublishesTextAndMarksThePostAsOurs(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishText, Message: "Olá"})

	done, err := f.run(t, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != fbdomain.JobPublished || done.FBPostID != "fb-page-1_post1" {
		t.Fatalf("job = %+v", done)
	}
	if post := f.posts.byFBID["fb-page-1_post1"]; post == nil || !post.CreatedByApp || post.Message != "Olá" {
		t.Fatalf("mirror = %+v", post)
	}
}

func TestWorkerSchedulesAScheduledPost(t *testing.T) {
	f := newPostFixture()
	at := f.now.Add(2 * time.Hour)
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishLink, Link: "https://loja.example", ScheduledAt: &at})

	done, err := f.run(t, job.ID)
	if err != nil || done.Status != fbdomain.JobScheduled {
		t.Fatalf("job = %+v, %v", done, err)
	}
	if got := f.service.feedPosts[0]; got.ScheduledAt == nil || !got.ScheduledAt.Equal(at) || got.Link != "https://loja.example" {
		t.Fatalf("feed input = %+v", got)
	}
}

func TestSinglePhotoPublishesDirectly(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishPhoto, Message: "Foto", Media: []fbdomain.MediaRef{photoRef("https://r2/a.jpg")}})

	done, err := f.run(t, job.ID)
	if err != nil || done.Status != fbdomain.JobPublished || done.FBPostID != "fb-page-1_photo1" {
		t.Fatalf("job = %+v, %v", done, err)
	}
	if p := f.service.photos[0]; !p.Published || p.Caption != "Foto" || len(f.service.feedPosts) != 0 {
		t.Fatalf("photo = %+v, feed = %v", p, f.service.feedPosts)
	}
}

func TestScheduledPhotoGoesThroughTheFeedWithATemporaryPhoto(t *testing.T) {
	f := newPostFixture()
	at := f.now.Add(time.Hour)
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishPhoto, Media: []fbdomain.MediaRef{photoRef("https://r2/a.jpg")}, ScheduledAt: &at})

	done, err := f.run(t, job.ID)
	if err != nil || done.Status != fbdomain.JobScheduled {
		t.Fatalf("job = %+v, %v", done, err)
	}
	if p := f.service.photos[0]; p.Published || !p.Temporary {
		t.Fatalf("photo = %+v", p)
	}
	if feed := f.service.feedPosts[0]; len(feed.AttachedMedia) != 1 || feed.AttachedMedia[0] != "photo1" {
		t.Fatalf("feed = %+v", feed)
	}
}

func TestAlbumResumesFromTheLastUploadedPhoto(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishAlbum, Message: "Álbum", Media: []fbdomain.MediaRef{
		photoRef("https://r2/a.jpg"), photoRef("https://r2/b.jpg"), photoRef("https://r2/c.jpg"),
	}})
	f.service.photoErrs = []error{nil, &meta.Error{Code: meta.CodePageRateLimit}}

	first, err := f.run(t, job.ID)
	if err == nil || first.Status != fbdomain.JobQueued || len(first.Progress.PhotoIDs) != 1 {
		t.Fatalf("after the throttled upload: job = %+v, err = %v", first, err)
	}

	done, err := f.run(t, job.ID)
	if err != nil || done.Status != fbdomain.JobPublished {
		t.Fatalf("job = %+v, %v", done, err)
	}
	if len(f.service.photos) != 4 {
		t.Fatalf("uploads = %d, want the first photo uploaded once", len(f.service.photos))
	}
	feed := f.service.feedPosts[0]
	if len(feed.AttachedMedia) != 3 || feed.AttachedMedia[0] != "photo1" {
		t.Fatalf("attached = %v", feed.AttachedMedia)
	}
	for _, p := range f.service.photos {
		if p.Published {
			t.Fatal("album photos must upload unpublished")
		}
	}
}

func TestAmbiguousCreateFailsWithoutRetrying(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishText, Message: "x"})
	f.service.createErr = errTransport

	done, err := f.run(t, job.ID)
	if err != nil {
		t.Fatalf("an ambiguous failure must not be retried by the queue: %v", err)
	}
	if done.Status != fbdomain.JobFailed || !done.Ambiguous {
		t.Fatalf("job = %+v", done)
	}
}

func TestACrashMidCreateIsNeverRepeated(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishText, Message: "x"})
	stored := f.jobs.byID[job.ID]
	stored.Progress.Phase = fbdomain.PhaseCreating

	done, err := f.run(t, job.ID)
	if err != nil || done.Status != fbdomain.JobFailed || !done.Ambiguous || len(f.service.feedPosts) != 0 {
		t.Fatalf("job = %+v, creates = %d, err = %v", done, len(f.service.feedPosts), err)
	}
}

func TestRejectedCreateFailsWithMetasReason(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishText, Message: "x"})
	f.service.createErr = &meta.Error{Code: meta.CodeDuplicatePost, Message: "Duplicate status message"}

	done, err := f.run(t, job.ID)
	if err != nil || done.Status != fbdomain.JobFailed || done.ErrorCode != meta.CodeDuplicatePost || done.Ambiguous {
		t.Fatalf("job = %+v, %v", done, err)
	}
}

func TestAClaimedJobIsNotWorkedTwice(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishText, Message: "x"})
	f.jobs.byID[job.ID].Status = fbdomain.JobUploading

	if _, err := f.run(t, job.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.service.feedPosts) != 0 {
		t.Fatal("a job another worker holds was published again")
	}
}

func TestAJobOutOfTriesFails(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishText, Message: "x"})
	f.jobs.byID[job.ID].Attempts = fbdomain.MaxPublishTries

	done, err := f.run(t, job.ID)
	if err != nil || done.Status != fbdomain.JobFailed || len(f.service.feedPosts) != 0 {
		t.Fatalf("job = %+v, %v", done, err)
	}
}

func TestReaperRequeuesStaleJobs(t *testing.T) {
	f := newPostFixture()
	f.jobs.stale = []string{"job-a", "job-b"}

	if err := f.worker.Reap(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.queue.enqueued) != 2 {
		t.Fatalf("queue = %v", f.queue.enqueued)
	}
}

func TestListMirrorsWhatItReads(t *testing.T) {
	f := newPostFixture()
	created := f.now
	f.service.listed = []*fbdomain.RemotePost{{FBPostID: "fb-page-1_9", FromID: "fb-page-1", Message: "oi", CreatedTime: &created}}

	page, err := f.uc.List(context.Background(), "ws-1", "page-1", fbdomain.ListPublished, 0, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("page = %+v, %v", page, err)
	}
	if f.posts.byFBID["fb-page-1_9"] == nil {
		t.Fatal("listed post not mirrored")
	}
	if page.Items[0].Editable || !page.HasNext || page.NextCursor != "next" {
		t.Fatalf("view = %+v", page)
	}
	f.posts.byFBID["fb-page-1_9"].CreatedByApp = true
	again, _ := f.uc.List(context.Background(), "ws-1", "page-1", fbdomain.ListPublished, 0, "")
	if !again.Items[0].Editable {
		t.Fatal("an app-made post must list as editable")
	}
}

func TestPostOperationsStayOnTheirPage(t *testing.T) {
	f := newPostFixture()
	ctx := context.Background()

	if _, _, err := f.uc.Asset(ctx, "ws-1", "page-1", "other-page_1", false); !errors.Is(err, fbdomain.ErrPostNotFound) {
		t.Errorf("asset: %v", err)
	}
	if err := f.uc.Delete(ctx, "ws-1", "page-1", "other-page_1"); !errors.Is(err, fbdomain.ErrPostNotFound) {
		t.Errorf("delete: %v", err)
	}
	hide := true
	if err := f.uc.Update(ctx, "ws-1", "page-1", "other-page_1", fbdomain.PostUpdate{IsHidden: &hide}); !errors.Is(err, fbdomain.ErrPostNotFound) {
		t.Errorf("update: %v", err)
	}
	if len(f.service.deleted)+len(f.service.updates) != 0 {
		t.Fatal("a foreign post reached Graph")
	}
}

func TestEditingContentNeedsAnAppMadePostButHidingDoesNot(t *testing.T) {
	f := newPostFixture()
	ctx := context.Background()
	f.posts.byFBID["fb-page-1_1"] = &fbdomain.Post{FBPostID: "fb-page-1_1"}
	message := "novo"

	if err := f.uc.Update(ctx, "ws-1", "page-1", "fb-page-1_1", fbdomain.PostUpdate{Message: &message}); !errors.Is(err, fbdomain.ErrPostNotEditable) {
		t.Fatalf("edit: %v", err)
	}
	hide := true
	if err := f.uc.Update(ctx, "ws-1", "page-1", "fb-page-1_1", fbdomain.PostUpdate{IsHidden: &hide}); err != nil {
		t.Fatalf("hide: %v", err)
	}
	if !f.posts.byFBID["fb-page-1_1"].IsHidden {
		t.Fatal("hide not mirrored")
	}
	f.posts.byFBID["fb-page-1_1"].CreatedByApp = true
	if err := f.uc.Update(ctx, "ws-1", "page-1", "fb-page-1_1", fbdomain.PostUpdate{Message: &message}); err != nil || f.posts.byFBID["fb-page-1_1"].Message != "novo" {
		t.Fatalf("edit own: %v", err)
	}
	if err := f.uc.Update(ctx, "ws-1", "page-1", "fb-page-1_1", fbdomain.PostUpdate{}); !errors.Is(err, fbdomain.ErrInvalidPost) {
		t.Fatalf("empty update: %v", err)
	}
}

func TestDeleteThatMetaForbidsSaysSo(t *testing.T) {
	f := newPostFixture()
	f.service.deleteErr = &meta.Error{Code: meta.CodePermission, Message: "(#10) Application does not have permission"}

	if err := f.uc.Delete(context.Background(), "ws-1", "page-1", "fb-page-1_1"); !errors.Is(err, fbdomain.ErrDeleteNotPermitted) {
		t.Fatalf("got %v", err)
	}
}

func TestJobsAreReadOnlyThroughTheirPage(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishText, Message: "x"})
	f.pages.byID["page-2"] = &fbdomain.Page{ID: "page-2", WorkspaceID: "ws-1", Status: fbdomain.StatusConnected}

	if _, err := f.uc.Job(context.Background(), "ws-1", "page-2", job.ID); !errors.Is(err, fbdomain.ErrPublishJobNotFound) {
		t.Fatalf("got %v", err)
	}
	if got, err := f.uc.Job(context.Background(), "ws-1", "page-1", job.ID); err != nil || got.ID != job.ID {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestPublishFailuresRetryUnlessTheJobIsGone(t *testing.T) {
	if ClassifyPublishFailure(fbdomain.ErrPublishJobNotFound) != webhook_usecase.DispositionDrop {
		t.Error("a missing job cannot be retried")
	}
	if ClassifyPublishFailure(errPublishRetry) != webhook_usecase.DispositionRetry || ClassifyPublishFailure(errTransport) != webhook_usecase.DispositionRetry {
		t.Error("throttled and storage failures retry")
	}
}
