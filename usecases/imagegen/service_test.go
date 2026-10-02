package imagegen_usecase

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"vozko/domain/imagegen"
	"vozko/domain/media"
	webhook_usecase "vozko/usecases/webhook"
)

var clock = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

type fakeGenerator struct {
	calls      int
	cost       int64
	err        error
	references []imagegen.ReferenceImage
}

func (g *fakeGenerator) Generate(_ context.Context, _ imagegen.Request, references []imagegen.ReferenceImage) (*imagegen.GeneratedImage, error) {
	g.calls++
	g.references = references
	if g.err != nil {
		return nil, g.err
	}
	return &imagegen.GeneratedImage{Bytes: []byte("jpg"), MIMEType: "image/jpeg", Model: "openai/gpt-image-2.5-flare", ProviderCostMicros: g.cost}, nil
}

type fakeFunds struct{ err error }

func (f *fakeFunds) Check(string) error { return f.err }

type billedEvent struct {
	model string
	cost  int64
}

type fakeAIBilling struct{ events []billedEvent }

func (b *fakeAIBilling) Publish(_, model string, _, _ int, cost int64) {
	b.events = append(b.events, billedEvent{model: model, cost: cost})
}

type fakeUploader struct {
	names []string
	kinds []media.MediaType
	err   error
}

func (u *fakeUploader) UploadMedia(ws string, _ []byte, name string, kind media.MediaType, _ string) (media.Media, error) {
	u.names = append(u.names, name)
	u.kinds = append(u.kinds, kind)
	if u.err != nil {
		return media.Media{}, u.err
	}
	return media.Media{ID: "m-1", WorkspaceID: ws, URL: "https://cdn/" + name, Type: kind}, nil
}

type fakeLibrary struct {
	items map[string]media.Media
	err   error
}

func newFakeLibrary() *fakeLibrary {
	return &fakeLibrary{items: map[string]media.Media{
		"ref-1": {ID: "ref-1", WorkspaceID: "ws-1", URL: "https://cdn/ref-1.png", Type: media.MediaTypeProductImage},
		"ref-2": {ID: "ref-2", WorkspaceID: "ws-1", URL: "https://cdn/ref-2.jpg", Type: media.MediaTypeProductImage},
		"clip":  {ID: "clip", WorkspaceID: "ws-1", URL: "https://cdn/clip.mp4", Type: media.MediaTypeProductVideo},
		"other": {ID: "other", WorkspaceID: "ws-2", URL: "https://cdn/other.png", Type: media.MediaTypeProductImage},
	}}
}

func (l *fakeLibrary) GetMedia(workspaceID, mediaID string) (*media.Media, error) {
	if l.err != nil {
		return nil, l.err
	}
	item, ok := l.items[mediaID]
	if !ok || item.WorkspaceID != workspaceID {
		return nil, media.ErrMediaNotFound
	}
	return &item, nil
}

type fakeQueue struct {
	ids []string
	err error
}

func (q *fakeQueue) Enqueue(id string) error {
	if q.err != nil {
		return q.err
	}
	q.ids = append(q.ids, id)
	return nil
}

type fakeJobs struct {
	mu        sync.Mutex
	jobs      map[string]*imagegen.Job
	seq       int
	createErr error
	claimErr  error
	gets      []imagegen.Status
	staleCut  time.Time
}

func newFakeJobs() *fakeJobs { return &fakeJobs{jobs: map[string]*imagegen.Job{}} }

func (r *fakeJobs) Create(_ context.Context, job *imagegen.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	r.seq++
	job.ID = "job-" + string(rune('0'+r.seq))
	job.CreatedAt, job.UpdatedAt = clock, clock
	copied := *job
	r.jobs[job.ID] = &copied
	return nil
}

func (r *fakeJobs) Get(_ context.Context, workspaceID, id string) (*imagegen.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok || job.WorkspaceID != workspaceID {
		return nil, imagegen.ErrJobNotFound
	}
	if len(r.gets) > 0 {
		job.Status, r.gets = r.gets[0], r.gets[1:]
	}
	copied := *job
	return &copied, nil
}

func (r *fakeJobs) FindActive(_ context.Context, workspaceID, requestedBy, fingerprint string, since time.Time) (*imagegen.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, job := range r.jobs {
		if job.WorkspaceID == workspaceID && job.RequestedBy == requestedBy && job.Fingerprint == fingerprint &&
			!job.Status.Terminal() && !job.CreatedAt.Before(since) {
			copied := *job
			return &copied, nil
		}
	}
	return nil, imagegen.ErrJobNotFound
}

func (r *fakeJobs) Claim(_ context.Context, id string) (*imagegen.Job, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.claimErr != nil {
		return nil, false, r.claimErr
	}
	job, ok := r.jobs[id]
	if !ok || job.Status != imagegen.StatusQueued {
		return nil, false, nil
	}
	job.Status = imagegen.StatusRunning
	job.Attempts++
	copied := *job
	return &copied, true, nil
}

func (r *fakeJobs) MarkDone(_ context.Context, id string, result imagegen.Result, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok || job.Status != imagegen.StatusRunning {
		return imagegen.ErrJobNotActive
	}
	job.Status, job.MediaID, job.MediaURL, job.Model, job.FinishedAt = imagegen.StatusDone, result.MediaID, result.MediaURL, result.Model, &at
	return nil
}

func (r *fakeJobs) MarkFailed(_ context.Context, id string, code imagegen.FailureCode, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok || job.Status.Terminal() {
		return imagegen.ErrJobNotActive
	}
	job.Status, job.FailureCode, job.FinishedAt = imagegen.StatusFailed, code, &at
	return nil
}

func (r *fakeJobs) FailStale(_ context.Context, before time.Time, _ int) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.staleCut = before
	var ids []string
	for id, job := range r.jobs {
		if !job.Status.Terminal() && job.CreatedAt.Before(before) {
			job.Status, job.FailureCode = imagegen.StatusFailed, imagegen.FailureTimedOut
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (r *fakeJobs) job(id string) imagegen.Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return *r.jobs[id]
}

type fixture struct {
	lib   *fakeLibrary
	gen   *fakeGenerator
	funds *fakeFunds
	bill  *fakeAIBilling
	up    *fakeUploader
	queue *fakeQueue
	jobs  *fakeJobs
	svc   *Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{lib: newFakeLibrary(), gen: &fakeGenerator{}, funds: &fakeFunds{}, bill: &fakeAIBilling{}, up: &fakeUploader{}, queue: &fakeQueue{}, jobs: newFakeJobs()}
	svc, err := NewService(Deps{
		Generator: f.gen, Jobs: f.jobs, Queue: f.queue, Funds: f.funds, Billing: f.bill, Uploader: f.up, References: f.lib, CostCeilingMicros: 250_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return clock }
	svc.poll = pollPolicy{first: time.Millisecond, factor: 1.5, max: 2 * time.Millisecond}
	f.svc = svc
	return f
}

func imageRequest() imagegen.Request {
	return imagegen.Request{WorkspaceID: "ws-1", Prompt: "pizza artesanal", Aspect: imagegen.AspectSquare}
}

func (f *fixture) requestAndProcess(t *testing.T) imagegen.Job {
	t.Helper()
	job, err := f.svc.Request(context.Background(), imageRequest(), "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Process(context.Background(), &imagegen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	return f.jobs.job(job.ID)
}

func TestGeneratedImageIsBilledAsAIAndStoredInTheMediaLibrary(t *testing.T) {
	f := newFixture(t)
	f.gen.cost = 53_000
	job := f.requestAndProcess(t)
	if job.Status != imagegen.StatusDone || job.MediaID != "m-1" || job.MediaURL == "" || job.Model != "openai/gpt-image-2.5-flare" || job.FinishedAt == nil {
		t.Fatalf("job %+v", job)
	}
	if len(f.up.kinds) != 1 || f.up.kinds[0] != media.MediaTypeProductImage || len(f.bill.events) != 1 || f.bill.events[0].cost != 53_000 {
		t.Fatalf("uploaded %+v billed %+v", f.up.kinds, f.bill.events)
	}
}

func TestUnknownProviderCostIsBilledAtTheCeilingNeverFree(t *testing.T) {
	f := newFixture(t)
	f.requestAndProcess(t)
	if len(f.bill.events) != 1 || f.bill.events[0].cost != 250_000 {
		t.Fatalf("billed %+v", f.bill.events)
	}
}

func TestNoJobWithoutFunds(t *testing.T) {
	f := newFixture(t)
	f.funds.err = errors.New("no balance")
	if _, err := f.svc.Request(context.Background(), imageRequest(), "u-1"); err == nil {
		t.Fatal("request accepted without funds")
	}
	if len(f.jobs.jobs) != 0 || len(f.queue.ids) != 0 || f.gen.calls != 0 {
		t.Fatalf("jobs %d queued %d generated %d", len(f.jobs.jobs), len(f.queue.ids), f.gen.calls)
	}
}

func TestInvalidRequestsNeverReachTheQueue(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Request(context.Background(), imagegen.Request{WorkspaceID: "ws-1", Aspect: "wide"}, "u-1"); !errors.Is(err, imagegen.ErrInvalidRequest) {
		t.Fatalf("got %v", err)
	}
	if _, err := f.svc.Request(context.Background(), imageRequest(), ""); !errors.Is(err, imagegen.ErrRequesterRequired) {
		t.Fatalf("got %v", err)
	}
	if len(f.jobs.jobs) != 0 || len(f.queue.ids) != 0 {
		t.Fatal("an invalid request was stored")
	}
}

func TestADoubleClickReturnsTheJobAlreadyRunning(t *testing.T) {
	f := newFixture(t)
	first, err := f.svc.Request(context.Background(), imageRequest(), "u-1")
	if err != nil {
		t.Fatal(err)
	}
	again := imageRequest()
	again.Prompt = "  pizza   artesanal "
	second, err := f.svc.Request(context.Background(), again, "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || len(f.queue.ids) != 1 || len(f.jobs.jobs) != 1 {
		t.Fatalf("first %s second %s queued %v", first.ID, second.ID, f.queue.ids)
	}
}

func TestAnotherUserGetsTheirOwnJob(t *testing.T) {
	f := newFixture(t)
	first, _ := f.svc.Request(context.Background(), imageRequest(), "u-1")
	second, err := f.svc.Request(context.Background(), imageRequest(), "u-2")
	if err != nil || first.ID == second.ID || len(f.queue.ids) != 2 {
		t.Fatalf("first %s second %+v err %v", first.ID, second, err)
	}
}

func TestAFinishedJobDoesNotBlockANewOne(t *testing.T) {
	f := newFixture(t)
	done := f.requestAndProcess(t)
	again, err := f.svc.Request(context.Background(), imageRequest(), "u-1")
	if err != nil || again.ID == done.ID {
		t.Fatalf("again %+v err %v", again, err)
	}
}

func TestLosingTheCreateRaceReturnsTheWinner(t *testing.T) {
	f := newFixture(t)
	winner, _ := f.svc.Request(context.Background(), imageRequest(), "u-1")
	f.jobs.mu.Lock()
	f.jobs.jobs[winner.ID].CreatedAt = clock.Add(-11 * time.Minute)
	f.jobs.createErr = imagegen.ErrDuplicateActiveJob
	f.jobs.mu.Unlock()
	got, err := f.svc.Request(context.Background(), imageRequest(), "u-1")
	if err != nil || got.ID != winner.ID || len(f.queue.ids) != 1 {
		t.Fatalf("got %+v err %v queued %v", got, err, f.queue.ids)
	}
}

func TestLosingTheCreateRaceToAFinishedJobIsAConflict(t *testing.T) {
	f := newFixture(t)
	f.jobs.createErr = imagegen.ErrDuplicateActiveJob
	if _, err := f.svc.Request(context.Background(), imageRequest(), "u-1"); !errors.Is(err, imagegen.ErrDuplicateActiveJob) {
		t.Fatalf("got %v", err)
	}
	if len(f.queue.ids) != 0 {
		t.Fatalf("queued %v", f.queue.ids)
	}
}

func TestAJobThatCannotBeQueuedIsFailedNotLeftWaiting(t *testing.T) {
	f := newFixture(t)
	f.queue.err = errors.New("rabbit down")
	if _, err := f.svc.Request(context.Background(), imageRequest(), "u-1"); err == nil {
		t.Fatal("enqueue failure hidden")
	}
	job := f.jobs.job("job-1")
	if job.Status != imagegen.StatusFailed || job.FailureCode != imagegen.FailureEnqueue {
		t.Fatalf("job %+v", job)
	}
}

func TestAJobIsGeneratedOnlyOnce(t *testing.T) {
	f := newFixture(t)
	job := f.requestAndProcess(t)
	if err := f.svc.Process(context.Background(), &imagegen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	if f.gen.calls != 1 || len(f.bill.events) != 1 {
		t.Fatalf("generated %d billed %d", f.gen.calls, len(f.bill.events))
	}
}

func TestAClaimFailureIsRetried(t *testing.T) {
	f := newFixture(t)
	job, _ := f.svc.Request(context.Background(), imageRequest(), "u-1")
	f.jobs.claimErr = errors.New("db down")
	err := f.svc.Process(context.Background(), &imagegen.QueueMessage{JobID: job.ID})
	if err == nil || Classify(err) != webhook_usecase.DispositionRetry || f.gen.calls != 0 {
		t.Fatalf("err %v calls %d", err, f.gen.calls)
	}
}

func TestAMessageWithoutAJobIsDropped(t *testing.T) {
	f := newFixture(t)
	err := f.svc.Process(context.Background(), &imagegen.QueueMessage{JobID: " "})
	if Classify(err) != webhook_usecase.DispositionDrop {
		t.Fatalf("err %v", err)
	}
}

func TestAGenerationFailureFailsTheJobWithoutBilling(t *testing.T) {
	f := newFixture(t)
	f.gen.err = imagegen.ErrGenerationFailed
	job := f.requestAndProcess(t)
	if job.Status != imagegen.StatusFailed || job.FailureCode != imagegen.FailureGeneration || len(f.bill.events) != 0 {
		t.Fatalf("job %+v billed %+v", job, f.bill.events)
	}
}

func TestABilledImageThatCannotBeStoredFailsAsStorage(t *testing.T) {
	f := newFixture(t)
	f.up.err = errors.New("s3 down")
	job := f.requestAndProcess(t)
	if job.Status != imagegen.StatusFailed || job.FailureCode != imagegen.FailureStorage || len(f.bill.events) != 1 {
		t.Fatalf("job %+v billed %+v", job, f.bill.events)
	}
}

func TestFundsGoneBeforeTheWorkerRunsStopsTheGeneration(t *testing.T) {
	f := newFixture(t)
	job, _ := f.svc.Request(context.Background(), imageRequest(), "u-1")
	f.funds.err = errors.New("no balance")
	if err := f.svc.Process(context.Background(), &imagegen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	got := f.jobs.job(job.ID)
	if got.Status != imagegen.StatusFailed || got.FailureCode != imagegen.FailureInsufficientFunds || f.gen.calls != 0 {
		t.Fatalf("job %+v calls %d", got, f.gen.calls)
	}
}

func TestReapTimesOutOldJobsAndNeverRequeues(t *testing.T) {
	f := newFixture(t)
	job, _ := f.svc.Request(context.Background(), imageRequest(), "u-1")
	f.jobs.mu.Lock()
	f.jobs.jobs[job.ID].CreatedAt = clock.Add(-11 * time.Minute)
	f.jobs.mu.Unlock()
	if err := f.svc.Reap(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := f.jobs.job(job.ID)
	if got.Status != imagegen.StatusFailed || got.FailureCode != imagegen.FailureTimedOut || len(f.queue.ids) != 1 {
		t.Fatalf("job %+v queued %v", got, f.queue.ids)
	}
	if !f.jobs.staleCut.Equal(clock.Add(-10 * time.Minute)) {
		t.Fatalf("cutoff %s", f.jobs.staleCut)
	}
}

func TestGetIsScopedToTheWorkspace(t *testing.T) {
	f := newFixture(t)
	job, _ := f.svc.Request(context.Background(), imageRequest(), "u-1")
	if _, err := f.svc.Get(context.Background(), "ws-2", job.ID); !errors.Is(err, imagegen.ErrJobNotFound) {
		t.Fatalf("got %v", err)
	}
	if _, err := f.svc.Get(context.Background(), "", job.ID); !errors.Is(err, imagegen.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
}

func TestWaitReturnsOnceTheJobFinishes(t *testing.T) {
	f := newFixture(t)
	job, _ := f.svc.Request(context.Background(), imageRequest(), "u-1")
	f.jobs.gets = []imagegen.Status{imagegen.StatusQueued, imagegen.StatusRunning, imagegen.StatusDone}
	got, err := f.svc.Wait(context.Background(), "ws-1", job.ID)
	if err != nil || got.Status != imagegen.StatusDone {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestWaitGivesUpWhenTheContextEnds(t *testing.T) {
	f := newFixture(t)
	job, _ := f.svc.Request(context.Background(), imageRequest(), "u-1")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := f.svc.Wait(ctx, "ws-1", job.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestWaitRefusesAnUnknownStatus(t *testing.T) {
	f := newFixture(t)
	job, _ := f.svc.Request(context.Background(), imageRequest(), "u-1")
	f.jobs.gets = []imagegen.Status{"paused"}
	if _, err := f.svc.Wait(context.Background(), "ws-1", job.ID); !errors.Is(err, imagegen.ErrUnknownJobStatus) {
		t.Fatalf("got %v", err)
	}
}

func TestPollingBacksOffByHalfUpToFiveSeconds(t *testing.T) {
	policy := defaultPoll
	delay := policy.first
	var got []time.Duration
	for i := 0; i < 6; i++ {
		got = append(got, delay)
		delay = policy.next(delay)
	}
	want := []time.Duration{time.Second, 1500 * time.Millisecond, 2250 * time.Millisecond, 3375 * time.Millisecond, 5 * time.Second, 5 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delays %v, want %v", got, want)
		}
	}
}

func TestTheServiceRefusesAFreeCeilingOrMissingParts(t *testing.T) {
	full := Deps{Generator: &fakeGenerator{}, Jobs: newFakeJobs(), Queue: &fakeQueue{}, Funds: &fakeFunds{}, Billing: &fakeAIBilling{}, Uploader: &fakeUploader{}, References: newFakeLibrary(), CostCeilingMicros: 1}
	if _, err := NewService(full); err != nil {
		t.Fatal(err)
	}
	free := full
	free.CostCeilingMicros = 0
	if _, err := NewService(free); !errors.Is(err, ErrInvalidCostCeiling) {
		t.Fatalf("got %v", err)
	}
	for _, drop := range []func(*Deps){func(d *Deps) { d.Queue = nil }, func(d *Deps) { d.References = nil }} {
		missing := full
		drop(&missing)
		if _, err := NewService(missing); !errors.Is(err, ErrMissingDependency) {
			t.Fatalf("got %v", err)
		}
	}
}

func referencedRequest(ids ...string) imagegen.Request {
	req := imageRequest()
	req.ReferenceMediaIDs = ids
	return req
}

func referenceCode(t *testing.T, err error) string {
	t.Helper()
	var invalid *imagegen.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected a field issue, got %v", err)
	}
	return invalid.Codes()[imagegen.FieldReferences]
}

func TestReferenceImagesFromTheLibraryReachTheGenerator(t *testing.T) {
	f := newFixture(t)
	job, err := f.svc.Request(context.Background(), referencedRequest("ref-2", "ref-1"), "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Process(context.Background(), &imagegen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	want := []imagegen.ReferenceImage{{MediaID: "ref-2", URL: "https://cdn/ref-2.jpg"}, {MediaID: "ref-1", URL: "https://cdn/ref-1.png"}}
	if !reflect.DeepEqual(f.gen.references, want) {
		t.Fatalf("generator got %+v", f.gen.references)
	}
	stored := f.jobs.job(job.ID)
	if stored.Status != imagegen.StatusDone || !reflect.DeepEqual(stored.ReferenceMediaIDs, []string{"ref-2", "ref-1"}) {
		t.Fatalf("job %+v", stored)
	}
}

func TestDifferentReferencesAreADifferentJob(t *testing.T) {
	f := newFixture(t)
	plain, _ := f.svc.Request(context.Background(), imageRequest(), "u-1")
	referenced, err := f.svc.Request(context.Background(), referencedRequest("ref-1"), "u-1")
	if err != nil || plain.ID == referenced.ID || len(f.queue.ids) != 2 {
		t.Fatalf("plain %s referenced %+v err %v", plain.ID, referenced, err)
	}
}

func TestOnlyImagesOfThisWorkspaceCanBeReferences(t *testing.T) {
	cases := map[string]string{"missing": imagegen.CodeNotFound, "other": imagegen.CodeNotFound, "clip": imagegen.CodeNotImage}
	for id, code := range cases {
		f := newFixture(t)
		_, err := f.svc.Request(context.Background(), referencedRequest("ref-1", id), "u-1")
		if got := referenceCode(t, err); got != code {
			t.Fatalf("%s: code %q, want %q", id, got, code)
		}
		if err := f.svc.Check(referencedRequest(id)); referenceCode(t, err) != code {
			t.Fatalf("%s: check let it through: %v", id, err)
		}
		if len(f.jobs.jobs) != 0 || len(f.queue.ids) != 0 {
			t.Fatalf("%s: a job was stored", id)
		}
	}
}

func TestALibraryOutageIsNotMistakenForAMissingReference(t *testing.T) {
	f := newFixture(t)
	f.lib.err = errors.New("db down")
	_, err := f.svc.Request(context.Background(), referencedRequest("ref-1"), "u-1")
	var invalid *imagegen.ValidationError
	if err == nil || errors.As(err, &invalid) || len(f.jobs.jobs) != 0 {
		t.Fatalf("got %v", err)
	}
}

func TestAReferenceRemovedBeforeTheWorkerRunsFailsTheJobUnbilled(t *testing.T) {
	f := newFixture(t)
	job, err := f.svc.Request(context.Background(), referencedRequest("ref-1"), "u-1")
	if err != nil {
		t.Fatal(err)
	}
	delete(f.lib.items, "ref-1")
	if err := f.svc.Process(context.Background(), &imagegen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	got := f.jobs.job(job.ID)
	if got.Status != imagegen.StatusFailed || got.FailureCode != imagegen.FailureReferenceUnavailable || f.gen.calls != 0 || len(f.bill.events) != 0 {
		t.Fatalf("job %+v calls %d billed %v", got, f.gen.calls, f.bill.events)
	}
}

func TestALibraryOutageAtTheWorkerFailsTheGenerationNotTheReference(t *testing.T) {
	f := newFixture(t)
	job, _ := f.svc.Request(context.Background(), referencedRequest("ref-1"), "u-1")
	f.lib.err = errors.New("db down")
	if err := f.svc.Process(context.Background(), &imagegen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	got := f.jobs.job(job.ID)
	if got.Status != imagegen.StatusFailed || got.FailureCode != imagegen.FailureGeneration || f.gen.calls != 0 {
		t.Fatalf("job %+v calls %d", got, f.gen.calls)
	}
}

func TestReferencesResolvesInTheGivenOrder(t *testing.T) {
	f := newFixture(t)
	refs, err := f.svc.References("ws-1", []string{"ref-1", "ref-2"})
	if err != nil || len(refs) != 2 || refs[0].MediaID != "ref-1" || refs[1].URL != "https://cdn/ref-2.jpg" {
		t.Fatalf("refs %+v err %v", refs, err)
	}
	if _, err := f.svc.References("ws-2", []string{"ref-1"}); referenceCode(t, err) != imagegen.CodeNotFound {
		t.Fatalf("got %v", err)
	}
}
