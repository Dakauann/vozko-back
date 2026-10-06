package mediagen_usecase

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"vozko/domain/media"
	"vozko/domain/mediagen"
	webhook_usecase "vozko/usecases/webhook"
)

var clock = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

type fakeGenerator struct {
	calls      int
	model      string
	cost       int64
	err        error
	references []mediagen.Source
	output     string
	unpriced   bool
	generation string
}

func (g *fakeGenerator) Generate(_ context.Context, req mediagen.Request, references []mediagen.Source) (*mediagen.Output, error) {
	g.calls++
	g.model = req.Model
	g.references = references
	if g.err != nil {
		return nil, g.err
	}
	model := g.output
	if model == "" {
		model = "openai/gpt-image-2.5-flare"
	}
	return &mediagen.Output{Bytes: []byte("bytes"), MIMEType: "application/octet-stream", Model: model, ProviderCostMicros: g.cost, CostReported: !g.unpriced, GenerationID: g.generation}, nil
}

const (
	imageModel = "openai/gpt-image-2.5-flare"
	musicModel = "google/lyria-3-clip-preview"
)

type fakeCatalog struct {
	models []mediagen.Model
	audio  []mediagen.Model
	err    error
}

func (c *fakeCatalog) Models(_ context.Context, kind mediagen.Kind) ([]mediagen.Model, error) {
	if kind == mediagen.KindImage {
		return c.models, c.err
	}
	return c.audio, c.err
}

func newFakeCatalog() *fakeCatalog {
	return &fakeCatalog{
		models: []mediagen.Model{{ID: imageModel, Name: "GPT Image 2.5 Flare"}, {ID: "google/gemini-3-pro-image", Name: "Gemini 3 Pro Image"}},
		audio:  []mediagen.Model{{ID: musicModel, Name: "Lyria 3 Clip"}},
	}
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
		"song":  {ID: "song", WorkspaceID: "ws-1", URL: "https://cdn/song.m4a", Type: media.MediaTypeAudio},
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
	ids    []string
	topics []string
	err    error
}

func (q *fakeQueue) Enqueue(job *mediagen.Job) error {
	if q.err != nil {
		return q.err
	}
	q.ids = append(q.ids, job.ID)
	q.topics = append(q.topics, job.Kind.Topic())
	return nil
}

type fakeJobs struct {
	mu        sync.Mutex
	jobs      map[string]*mediagen.Job
	seq       int
	createErr error
	claimErr  error
	gets      []mediagen.Status
	staleCut  time.Time
}

func newFakeJobs() *fakeJobs { return &fakeJobs{jobs: map[string]*mediagen.Job{}} }

func (r *fakeJobs) Create(_ context.Context, job *mediagen.Job) error {
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

func (r *fakeJobs) Get(_ context.Context, workspaceID, id string) (*mediagen.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok || job.WorkspaceID != workspaceID {
		return nil, mediagen.ErrJobNotFound
	}
	if len(r.gets) > 0 {
		job.Status, r.gets = r.gets[0], r.gets[1:]
	}
	copied := *job
	return &copied, nil
}

func (r *fakeJobs) FindActive(_ context.Context, workspaceID, requestedBy, fingerprint string, since time.Time) (*mediagen.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, job := range r.jobs {
		if job.WorkspaceID == workspaceID && job.RequestedBy == requestedBy && job.Fingerprint == fingerprint &&
			!job.Status.Terminal() && !job.CreatedAt.Before(since) {
			copied := *job
			return &copied, nil
		}
	}
	return nil, mediagen.ErrJobNotFound
}

func (r *fakeJobs) Claim(_ context.Context, id string) (*mediagen.Job, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.claimErr != nil {
		return nil, false, r.claimErr
	}
	job, ok := r.jobs[id]
	if !ok || job.Status != mediagen.StatusQueued {
		return nil, false, nil
	}
	job.Status = mediagen.StatusRunning
	job.Attempts++
	copied := *job
	return &copied, true, nil
}

func (r *fakeJobs) MarkDone(_ context.Context, id string, result mediagen.Result, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok || job.Status != mediagen.StatusRunning {
		return mediagen.ErrJobNotActive
	}
	job.Status, job.MediaID, job.MediaURL, job.Model, job.FinishedAt = mediagen.StatusDone, result.MediaID, result.MediaURL, result.Model, &at
	return nil
}

func (r *fakeJobs) MarkSettling(_ context.Context, id string, settlement mediagen.Settlement, _ time.Time) error {
	if err := settlement.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok || job.Status != mediagen.StatusRunning {
		return mediagen.ErrJobNotActive
	}
	job.Status, job.GenerationID, job.FailureCode = mediagen.StatusSettling, settlement.GenerationID, settlement.Failure
	if settlement.Result != nil {
		job.MediaID, job.MediaURL, job.Model = settlement.Result.MediaID, settlement.Result.MediaURL, settlement.Result.Model
	}
	return nil
}

func (r *fakeJobs) ListSettling(_ context.Context, createdAfter time.Time, _ int) ([]*mediagen.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*mediagen.Job
	for _, job := range r.jobs {
		if job.Status == mediagen.StatusSettling && !job.CreatedAt.Before(createdAfter) {
			copied := *job
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (r *fakeJobs) Settle(_ context.Context, id string, at time.Time) (*mediagen.Job, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok || job.Status != mediagen.StatusSettling {
		return nil, false, nil
	}
	job.Status, job.FinishedAt = mediagen.StatusFailed, &at
	if job.MediaID != "" {
		job.Status = mediagen.StatusDone
	}
	copied := *job
	return &copied, true, nil
}

func (r *fakeJobs) ExpireSettling(_ context.Context, createdBefore time.Time, _ int) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ids []string
	for id, job := range r.jobs {
		if job.Status == mediagen.StatusSettling && job.CreatedAt.Before(createdBefore) {
			job.Status, job.FailureCode = mediagen.StatusFailed, mediagen.FailureCostUnreported
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (r *fakeJobs) CountActive(_ context.Context, workspaceID string, kinds []mediagen.Kind, since time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, job := range r.jobs {
		if job.WorkspaceID == workspaceID && slices.Contains(kinds, job.Kind) && (job.Status == mediagen.StatusQueued || job.Status == mediagen.StatusRunning) && !job.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

type fakeCharges struct {
	charged []string
	err     error
}

func (c *fakeCharges) Charge(_ context.Context, job *mediagen.Job) error {
	c.charged = append(c.charged, string(job.Kind))
	return c.err
}

type fakeCosts struct {
	costs map[string]int64
	asked []string
}

func (c *fakeCosts) CostMicros(_ context.Context, generationID string) (int64, bool) {
	c.asked = append(c.asked, generationID)
	cost, ok := c.costs[generationID]
	return cost, ok
}

func (r *fakeJobs) MarkFailed(_ context.Context, id string, code mediagen.FailureCode, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok || job.Status.Terminal() {
		return mediagen.ErrJobNotActive
	}
	job.Status, job.FailureCode, job.FinishedAt = mediagen.StatusFailed, code, &at
	return nil
}

func (r *fakeJobs) FailStale(_ context.Context, before time.Time, _ int) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.staleCut = before
	var ids []string
	for id, job := range r.jobs {
		if !job.Status.Terminal() && job.CreatedAt.Before(before) {
			job.Status, job.FailureCode = mediagen.StatusFailed, mediagen.FailureTimedOut
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (r *fakeJobs) job(id string) mediagen.Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return *r.jobs[id]
}

type fixture struct {
	lib     *fakeLibrary
	cat     *fakeCatalog
	gen     *fakeGenerator
	audio   *fakeGenerator
	video   *fakeGenerator
	costs   *fakeCosts
	late    *fakeCosts
	charges *fakeCharges
	funds   *fakeFunds
	bill    *fakeAIBilling
	up      *fakeUploader
	queue   *fakeQueue
	jobs    *fakeJobs
	svc     *Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{lib: newFakeLibrary(), cat: newFakeCatalog(), gen: &fakeGenerator{}, audio: &fakeGenerator{output: musicModel}, video: &fakeGenerator{output: "render"}, costs: &fakeCosts{costs: map[string]int64{}}, late: &fakeCosts{costs: map[string]int64{}}, charges: &fakeCharges{}, funds: &fakeFunds{}, bill: &fakeAIBilling{}, up: &fakeUploader{}, queue: &fakeQueue{}, jobs: newFakeJobs()}
	svc, err := NewService(Deps{
		Generators: map[mediagen.Kind]mediagen.Generator{
			mediagen.KindImage: f.gen, mediagen.KindMusic: f.audio, mediagen.KindVoice: f.audio, mediagen.KindVideo: f.video,
			mediagen.KindCutout: f.video, mediagen.KindCaptions: f.video, mediagen.KindDenoise: f.video,
		},
		Models: f.cat, Jobs: f.jobs, Queue: f.queue, Funds: f.funds, Billing: f.bill, Uploader: f.up, Library: f.lib,
		Costs: f.costs, LateCosts: f.late, Charges: f.charges,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return clock }
	svc.poll = pollPolicy{first: time.Millisecond, factor: 1.5, max: 2 * time.Millisecond}
	f.svc = svc
	return f
}

func imageRequest() mediagen.Request {
	return mediagen.Request{Kind: mediagen.KindImage, WorkspaceID: "ws-1", Model: imageModel, Prompt: "pizza artesanal", Aspect: mediagen.AspectSquare}
}

func (f *fixture) requestAndProcess(t *testing.T) mediagen.Job {
	t.Helper()
	job, err := f.svc.Request(context.Background(), imageRequest(), "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Process(context.Background(), &mediagen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	return f.jobs.job(job.ID)
}

func TestGeneratedImageIsBilledAsAIAndStoredInTheMediaLibrary(t *testing.T) {
	f := newFixture(t)
	f.gen.cost = 53_000
	job := f.requestAndProcess(t)
	if job.Status != mediagen.StatusDone || job.MediaID != "m-1" || job.MediaURL == "" || job.Model != "openai/gpt-image-2.5-flare" || job.FinishedAt == nil {
		t.Fatalf("job %+v", job)
	}
	if len(f.up.kinds) != 1 || f.up.kinds[0] != media.MediaTypeProductImage || len(f.bill.events) != 1 || f.bill.events[0].cost != 53_000 {
		t.Fatalf("uploaded %+v billed %+v", f.up.kinds, f.bill.events)
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
	if _, err := f.svc.Request(context.Background(), mediagen.Request{Kind: mediagen.KindImage, WorkspaceID: "ws-1", Aspect: "wide"}, "u-1"); !errors.Is(err, mediagen.ErrInvalidRequest) {
		t.Fatalf("got %v", err)
	}
	if _, err := f.svc.Request(context.Background(), imageRequest(), ""); !errors.Is(err, mediagen.ErrRequesterRequired) {
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
	f.jobs.createErr = mediagen.ErrDuplicateActiveJob
	f.jobs.mu.Unlock()
	got, err := f.svc.Request(context.Background(), imageRequest(), "u-1")
	if err != nil || got.ID != winner.ID || len(f.queue.ids) != 1 {
		t.Fatalf("got %+v err %v queued %v", got, err, f.queue.ids)
	}
}

func TestLosingTheCreateRaceToAFinishedJobIsAConflict(t *testing.T) {
	f := newFixture(t)
	f.jobs.createErr = mediagen.ErrDuplicateActiveJob
	if _, err := f.svc.Request(context.Background(), imageRequest(), "u-1"); !errors.Is(err, mediagen.ErrDuplicateActiveJob) {
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
	if job.Status != mediagen.StatusFailed || job.FailureCode != mediagen.FailureEnqueue {
		t.Fatalf("job %+v", job)
	}
}

func TestAJobIsGeneratedOnlyOnce(t *testing.T) {
	f := newFixture(t)
	job := f.requestAndProcess(t)
	if err := f.svc.Process(context.Background(), &mediagen.QueueMessage{JobID: job.ID}); err != nil {
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
	err := f.svc.Process(context.Background(), &mediagen.QueueMessage{JobID: job.ID})
	if err == nil || Classify(err) != webhook_usecase.DispositionRetry || f.gen.calls != 0 {
		t.Fatalf("err %v calls %d", err, f.gen.calls)
	}
}

func TestAMessageWithoutAJobIsDropped(t *testing.T) {
	f := newFixture(t)
	err := f.svc.Process(context.Background(), &mediagen.QueueMessage{JobID: " "})
	if Classify(err) != webhook_usecase.DispositionDrop {
		t.Fatalf("err %v", err)
	}
}

func TestAGenerationFailureFailsTheJobWithoutBilling(t *testing.T) {
	f := newFixture(t)
	f.gen.err = mediagen.ErrGenerationFailed
	job := f.requestAndProcess(t)
	if job.Status != mediagen.StatusFailed || job.FailureCode != mediagen.FailureGeneration || len(f.bill.events) != 0 {
		t.Fatalf("job %+v billed %+v", job, f.bill.events)
	}
}

func TestABilledImageThatCannotBeStoredFailsAsStorage(t *testing.T) {
	f := newFixture(t)
	f.up.err = errors.New("s3 down")
	job := f.requestAndProcess(t)
	if job.Status != mediagen.StatusFailed || job.FailureCode != mediagen.FailureStorage || len(f.bill.events) != 1 {
		t.Fatalf("job %+v billed %+v", job, f.bill.events)
	}
}

func TestFundsGoneBeforeTheWorkerRunsStopsTheGeneration(t *testing.T) {
	f := newFixture(t)
	job, _ := f.svc.Request(context.Background(), imageRequest(), "u-1")
	f.funds.err = errors.New("no balance")
	if err := f.svc.Process(context.Background(), &mediagen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	got := f.jobs.job(job.ID)
	if got.Status != mediagen.StatusFailed || got.FailureCode != mediagen.FailureInsufficientFunds || f.gen.calls != 0 {
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
	if got.Status != mediagen.StatusFailed || got.FailureCode != mediagen.FailureTimedOut || len(f.queue.ids) != 1 {
		t.Fatalf("job %+v queued %v", got, f.queue.ids)
	}
	if !f.jobs.staleCut.Equal(clock.Add(-10 * time.Minute)) {
		t.Fatalf("cutoff %s", f.jobs.staleCut)
	}
}

func TestGetIsScopedToTheWorkspace(t *testing.T) {
	f := newFixture(t)
	job, _ := f.svc.Request(context.Background(), imageRequest(), "u-1")
	if _, err := f.svc.Get(context.Background(), "ws-2", job.ID); !errors.Is(err, mediagen.ErrJobNotFound) {
		t.Fatalf("got %v", err)
	}
	if _, err := f.svc.Get(context.Background(), "", job.ID); !errors.Is(err, mediagen.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
}

func TestWaitReturnsOnceTheJobFinishes(t *testing.T) {
	f := newFixture(t)
	job, _ := f.svc.Request(context.Background(), imageRequest(), "u-1")
	f.jobs.gets = []mediagen.Status{mediagen.StatusQueued, mediagen.StatusRunning, mediagen.StatusDone}
	got, err := f.svc.Wait(context.Background(), "ws-1", job.ID)
	if err != nil || got.Status != mediagen.StatusDone {
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
	f.jobs.gets = []mediagen.Status{"paused"}
	if _, err := f.svc.Wait(context.Background(), "ws-1", job.ID); !errors.Is(err, mediagen.ErrUnknownJobStatus) {
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

func fullDeps() Deps {
	gen := &fakeGenerator{}
	return Deps{
		Generators: map[mediagen.Kind]mediagen.Generator{mediagen.KindImage: gen, mediagen.KindMusic: gen, mediagen.KindVoice: gen, mediagen.KindVideo: gen, mediagen.KindCutout: gen, mediagen.KindCaptions: gen, mediagen.KindDenoise: gen},
		Models:     newFakeCatalog(), Jobs: newFakeJobs(), Queue: &fakeQueue{}, Funds: &fakeFunds{}, Billing: &fakeAIBilling{}, Uploader: &fakeUploader{}, Library: newFakeLibrary(),
		Costs: &fakeCosts{}, LateCosts: &fakeCosts{}, Charges: &fakeCharges{},
	}
}

func TestTheServiceRefusesMissingParts(t *testing.T) {
	if _, err := NewService(fullDeps()); err != nil {
		t.Fatal(err)
	}
	drops := []func(*Deps){
		func(d *Deps) { d.Costs = nil },
		func(d *Deps) { d.LateCosts = nil },
		func(d *Deps) { d.Charges = nil },
		func(d *Deps) { d.Queue = nil },
		func(d *Deps) { d.Library = nil },
		func(d *Deps) { d.Models = nil },
		func(d *Deps) { delete(d.Generators, mediagen.KindVideo) },
	}
	for _, drop := range drops {
		missing := fullDeps()
		drop(&missing)
		if _, err := NewService(missing); !errors.Is(err, ErrMissingDependency) {
			t.Fatalf("got %v", err)
		}
	}
}

func referencedRequest(ids ...string) mediagen.Request {
	req := imageRequest()
	req.ReferenceMediaIDs = ids
	return req
}

func referenceCode(t *testing.T, err error) string {
	t.Helper()
	var invalid *mediagen.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected a field issue, got %v", err)
	}
	return invalid.Codes()[mediagen.FieldReferences]
}

func TestReferenceImagesFromTheLibraryReachTheGenerator(t *testing.T) {
	f := newFixture(t)
	job, err := f.svc.Request(context.Background(), referencedRequest("ref-2", "ref-1"), "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Process(context.Background(), &mediagen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	want := []mediagen.Source{{MediaID: "ref-2", URL: "https://cdn/ref-2.jpg", Type: media.MediaTypeProductImage}, {MediaID: "ref-1", URL: "https://cdn/ref-1.png", Type: media.MediaTypeProductImage}}
	if !reflect.DeepEqual(f.gen.references, want) {
		t.Fatalf("generator got %+v", f.gen.references)
	}
	stored := f.jobs.job(job.ID)
	if stored.Status != mediagen.StatusDone || !reflect.DeepEqual(stored.ReferenceMediaIDs, []string{"ref-2", "ref-1"}) {
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
	cases := map[string]string{"missing": mediagen.CodeNotFound, "other": mediagen.CodeNotFound, "clip": mediagen.CodeNotImage}
	for id, code := range cases {
		f := newFixture(t)
		_, err := f.svc.Request(context.Background(), referencedRequest("ref-1", id), "u-1")
		if got := referenceCode(t, err); got != code {
			t.Fatalf("%s: code %q, want %q", id, got, code)
		}
		if err := f.svc.Check(context.Background(), referencedRequest(id)); referenceCode(t, err) != code {
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
	var invalid *mediagen.ValidationError
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
	if err := f.svc.Process(context.Background(), &mediagen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	got := f.jobs.job(job.ID)
	if got.Status != mediagen.StatusFailed || got.FailureCode != mediagen.FailureReferenceUnavailable || f.gen.calls != 0 || len(f.bill.events) != 0 {
		t.Fatalf("job %+v calls %d billed %v", got, f.gen.calls, f.bill.events)
	}
}

func TestALibraryOutageAtTheWorkerFailsTheGenerationNotTheReference(t *testing.T) {
	f := newFixture(t)
	job, _ := f.svc.Request(context.Background(), referencedRequest("ref-1"), "u-1")
	f.lib.err = errors.New("db down")
	if err := f.svc.Process(context.Background(), &mediagen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	got := f.jobs.job(job.ID)
	if got.Status != mediagen.StatusFailed || got.FailureCode != mediagen.FailureGeneration || f.gen.calls != 0 {
		t.Fatalf("job %+v calls %d", got, f.gen.calls)
	}
}

func TestReferencesResolvesInTheGivenOrder(t *testing.T) {
	f := newFixture(t)
	refs, err := f.svc.Sources(mediagen.Request{Kind: mediagen.KindImage, WorkspaceID: "ws-1", ReferenceMediaIDs: []string{"ref-1", "ref-2"}})
	if err != nil || len(refs) != 2 || refs[0].MediaID != "ref-1" || refs[1].URL != "https://cdn/ref-2.jpg" {
		t.Fatalf("refs %+v err %v", refs, err)
	}
	if _, err := f.svc.Sources(mediagen.Request{Kind: mediagen.KindImage, WorkspaceID: "ws-2", ReferenceMediaIDs: []string{"ref-1"}}); referenceCode(t, err) != mediagen.CodeNotFound {
		t.Fatalf("got %v", err)
	}
}

func modelCode(t *testing.T, err error) string {
	t.Helper()
	var invalid *mediagen.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected a field issue, got %v", err)
	}
	return invalid.Codes()[mediagen.FieldModel]
}

func TestOnlyImageModelsFromTheCatalogCanGenerate(t *testing.T) {
	f := newFixture(t)
	req := imageRequest()
	req.Model = "anthropic/claude-sonnet-4"
	_, err := f.svc.Request(context.Background(), req, "u-1")
	if modelCode(t, err) != mediagen.CodeUnknown {
		t.Fatalf("got %v", err)
	}
	if modelCode(t, f.svc.Check(context.Background(), req)) != mediagen.CodeUnknown {
		t.Fatal("check accepted a model outside the catalog")
	}
	req.Model = ""
	if modelCode(t, f.svc.Check(context.Background(), req)) != mediagen.CodeRequired {
		t.Fatal("check accepted a request without a model")
	}
	if len(f.jobs.jobs) != 0 || len(f.queue.ids) != 0 {
		t.Fatal("a request with a wrong model was stored")
	}
}

func TestACatalogOutageRefusesInsteadOfGuessing(t *testing.T) {
	f := newFixture(t)
	f.cat.err = errors.New("openrouter down")
	if _, err := f.svc.Request(context.Background(), imageRequest(), "u-1"); !errors.Is(err, mediagen.ErrModelsUnavailable) {
		t.Fatalf("request while the catalog was unavailable: %v", err)
	}
	if err := f.svc.Check(context.Background(), imageRequest()); err == nil {
		t.Fatal("check passed while the catalog was unavailable")
	}
	if _, err := f.svc.Generates(context.Background(), mediagen.KindImage, imageModel); err == nil {
		t.Fatal("generates images answered while the catalog was unavailable")
	}
	if len(f.jobs.jobs) != 0 {
		t.Fatal("a job was stored")
	}
}

func TestTheChosenModelGeneratesTheImage(t *testing.T) {
	f := newFixture(t)
	req := imageRequest()
	req.Model = "google/gemini-3-pro-image"
	job, err := f.svc.Request(context.Background(), req, "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Process(context.Background(), &mediagen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	if f.gen.model != "google/gemini-3-pro-image" {
		t.Fatalf("generated with %q", f.gen.model)
	}
}

func TestContentIsCheckedWithoutAModel(t *testing.T) {
	f := newFixture(t)
	req := imageRequest()
	req.Model = ""
	if err := f.svc.CheckContent(req); err != nil {
		t.Fatal(err)
	}
	req.Prompt = ""
	if err := f.svc.CheckContent(req); !errors.Is(err, mediagen.ErrInvalidRequest) {
		t.Fatalf("got %v", err)
	}
	f.funds.err = errors.New("no balance")
	if err := f.svc.CheckContent(imageRequest()); err == nil {
		t.Fatal("content check passed without funds")
	}
}

func TestModelsKeepTheCatalogRanking(t *testing.T) {
	f := newFixture(t)
	models, err := f.svc.Models(context.Background(), mediagen.KindImage)
	if err != nil || len(models) != 2 || models[0].ID != imageModel {
		t.Fatalf("models %+v err %v", models, err)
	}
	f.cat.models = nil
	if _, err := f.svc.Models(context.Background(), mediagen.KindImage); !errors.Is(err, mediagen.ErrNoModels) {
		t.Fatalf("an empty catalog returned %v", err)
	}
}

func TestOnlyCatalogModelsGenerateImages(t *testing.T) {
	f := newFixture(t)
	for model, want := range map[string]bool{"google/gemini-3-pro-image": true, "anthropic/claude-sonnet-4": false, "": false} {
		got, err := f.svc.Generates(context.Background(), mediagen.KindImage, model)
		if err != nil || got != want {
			t.Fatalf("%q: %v %v", model, got, err)
		}
	}
}
