package report_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/export"
	"vozko/domain/messaging"
	"vozko/domain/report"
)

type fakeRepo struct {
	mu       sync.Mutex
	jobs     map[string]*report.Job
	reusable *report.Job
	created  []*report.Job
	seq      int
	failCode map[string]report.FailureCode
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{jobs: map[string]*report.Job{}, failCode: map[string]report.FailureCode{}}
}

func (r *fakeRepo) Create(job *report.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	job.ID = "job-" + string(rune('a'+r.seq-1))
	copied := *job
	r.jobs[job.ID] = &copied
	r.created = append(r.created, job)
	return nil
}

func (r *fakeRepo) GetByID(workspaceID, id string) (*report.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, found := r.jobs[id]
	if !found {
		return nil, report.ErrNotFound
	}
	if job.WorkspaceID != workspaceID {
		return nil, report.ErrForbidden
	}
	copied := *job
	return &copied, nil
}

func (r *fakeRepo) FindReusable(string, string, time.Time) (*report.Job, error) {
	if r.reusable == nil {
		return nil, report.ErrNotFound
	}
	return r.reusable, nil
}

func (r *fakeRepo) List(query report.ListQuery) (report.ListPage, error) {
	query.Normalize()
	r.mu.Lock()
	defer r.mu.Unlock()

	out := []report.Job{}
	for _, job := range r.jobs {
		if job.WorkspaceID != query.WorkspaceID {
			continue
		}
		if len(query.Kinds) > 0 && !containsKind(query.Kinds, job.Kind) {
			continue
		}
		if len(query.Statuses) > 0 && !containsStatus(query.Statuses, job.Status) {
			continue
		}
		out = append(out, *job)
	}

	total := int64(len(out))
	if query.Offset < len(out) {
		out = out[query.Offset:]
	} else {
		out = nil
	}
	if len(out) > query.Limit {
		out = out[:query.Limit]
	}
	return report.ListPage{Jobs: out, Total: total}, nil
}

func containsKind(list []report.Kind, value report.Kind) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func containsStatus(list []report.Status, value report.Status) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func (r *fakeRepo) MarkRunning(id string, at time.Time) error {
	return r.patch(id, func(job *report.Job) { job.Status = report.StatusRunning; job.StartedAt = &at })
}

func (r *fakeRepo) MarkDone(id, key, filename string, size int64, rows int64, at time.Time, expires *time.Time) error {
	return r.patch(id, func(job *report.Job) {
		job.Status = report.StatusDone
		job.ObjectKey = key
		job.Filename = filename
		job.SizeBytes = size
		job.RowCount = rows
		job.FinishedAt = &at
		job.ExpiresAt = expires
		job.Progress = 100
	})
}

func (r *fakeRepo) MarkFailed(id string, code report.FailureCode, at time.Time) error {
	r.mu.Lock()
	r.failCode[id] = code
	r.mu.Unlock()
	return r.patch(id, func(job *report.Job) {
		job.Status = report.StatusFailed
		job.FailureCode = code
		job.FinishedAt = &at
	})
}

func (r *fakeRepo) UpdateProgress(id string, percent int) error {
	return r.patch(id, func(job *report.Job) { job.Progress = percent })
}

func (r *fakeRepo) ExpireBefore(time.Time, int) (int64, error) { return 0, nil }

func (r *fakeRepo) patch(id string, mutate func(*report.Job)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, found := r.jobs[id]
	if !found {
		return report.ErrNotFound
	}
	mutate(job)
	return nil
}

type fakePublisher struct {
	published [][]byte
	err       error
}

func (p *fakePublisher) Publish(topic string, message []byte) error {
	if p.err != nil {
		return p.err
	}
	p.published = append(p.published, message)
	return nil
}

func (p *fakePublisher) PublishWithDelay(string, []byte, time.Duration) error { return nil }
func (p *fakePublisher) ValidateConnection() error                            { return nil }

type fakeStorage struct {
	mu       sync.Mutex
	objects  map[string][]byte
	types    map[string]string
	uploaErr error
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{objects: map[string][]byte{}, types: map[string]string{}}
}

func (s *fakeStorage) Upload(key string, data []byte, contentType string) error {
	if s.uploaErr != nil {
		return s.uploaErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = data
	s.types[key] = contentType
	return nil
}

func (s *fakeStorage) Download(_ context.Context, key string) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, found := s.objects[key]
	if !found {
		return nil, "", errors.New("no such object")
	}
	return data, s.types[key], nil
}

type stubRenderer struct {
	kind     report.Kind
	formats  []report.Format
	artifact report.Artifact
	err      error
	calls    int
	progress []int
}

func (r *stubRenderer) Kind() report.Kind        { return r.kind }
func (r *stubRenderer) Formats() []report.Format { return r.formats }

func (r *stubRenderer) Render(_ context.Context, _ report.Job, progress report.ProgressFunc) (report.Artifact, error) {
	r.calls++
	for _, percent := range r.progress {
		progress(percent)
	}
	return r.artifact, r.err
}

type fakeAck struct {
	acked      bool
	nacked     bool
	requeued   bool
	deliveries int
}

func (a *fakeAck) Ack() error { a.acked = true; return nil }
func (a *fakeAck) Nack(requeue bool) error {
	a.nacked = true
	a.requeued = requeue
	return nil
}
func (a *fakeAck) DeliveryCount() int { return a.deliveries }

const fixedNow = "2026-09-23T10:00:00Z"

func buildService(t *testing.T, renderer report.Renderer) (*Service, *fakeRepo, *fakePublisher, *fakeStorage) {
	t.Helper()
	repo := newFakeRepo()
	publisher := &fakePublisher{}
	storage := newFakeStorage()
	registry := report.NewRegistry()
	if renderer != nil {
		registry.Register(renderer)
	}
	service := NewService(repo, registry, publisher, storage)
	at, err := time.Parse(time.RFC3339, fixedNow)
	if err != nil {
		t.Fatalf("parse clock: %v", err)
	}
	service.SetClock(func() time.Time { return at })
	return service, repo, publisher, storage
}

func okRenderer() *stubRenderer {
	return &stubRenderer{
		kind:    report.KindAttendanceOverview,
		formats: []report.Format{report.FormatCSV},
		artifact: report.Artifact{
			Data:        []byte("a;b\r\n1;2\r\n"),
			ContentType: "text/csv; charset=utf-8",
			Filename:    "atendimento.csv",
			RowCount:    1,
		},
	}
}

func createJob(t *testing.T, service *Service) *report.Job {
	t.Helper()
	job, err := service.Create(CreateInput{
		WorkspaceID: "ws-1",
		RequestedBy: "user-1",
		Kind:        report.KindAttendanceOverview,
		Format:      report.FormatCSV,
		Locale:      "pt",
		Params:      json.RawMessage(`{"dateFrom":"2026-09-01"}`),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return job
}

func TestCreateQueuesAJobAndPublishesIt(t *testing.T) {
	service, _, publisher, _ := buildService(t, okRenderer())

	job := createJob(t, service)

	if job.Status != report.StatusQueued {
		t.Fatalf("status = %q, want %q", job.Status, report.StatusQueued)
	}
	if job.Fingerprint == "" {
		t.Fatal("the job has no fingerprint, so idempotency cannot work")
	}
	if len(publisher.published) != 1 {
		t.Fatalf("published %d messages, want 1", len(publisher.published))
	}

	var message report.QueueMessage
	if err := json.Unmarshal(publisher.published[0], &message); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	if message.JobID != job.ID || message.WorkspaceID != "ws-1" {
		t.Fatalf("message = %+v", message)
	}
}

func TestCreateRejectsAnUnknownKind(t *testing.T) {
	service, _, publisher, _ := buildService(t, okRenderer())

	_, err := service.Create(CreateInput{
		WorkspaceID: "ws-1",
		Kind:        report.Kind("not_a_report"),
		Format:      report.FormatCSV,
	})
	if !errors.Is(err, report.ErrUnknownKind) {
		t.Fatalf("err = %v, want ErrUnknownKind", err)
	}
	if len(publisher.published) != 0 {
		t.Fatal("an unknown kind must never reach the queue")
	}
}

func TestCreateRejectsAFormatTheRendererDoesNotSupport(t *testing.T) {
	service, _, _, _ := buildService(t, okRenderer())

	_, err := service.Create(CreateInput{
		WorkspaceID: "ws-1",
		Kind:        report.KindAttendanceOverview,
		Format:      report.FormatPDF,
	})
	if !errors.Is(err, report.ErrFormatUnsupported) {
		t.Fatalf("err = %v, want ErrFormatUnsupported", err)
	}
}

func TestCreateRejectsOversizedParams(t *testing.T) {
	service, _, _, _ := buildService(t, okRenderer())

	_, err := service.Create(CreateInput{
		WorkspaceID: "ws-1",
		Kind:        report.KindAttendanceOverview,
		Format:      report.FormatCSV,
		Params:      json.RawMessage(`{"x":"` + strings.Repeat("y", report.MaxParamsBytes) + `"}`),
	})
	if !errors.Is(err, report.ErrParamsTooLarge) {
		t.Fatalf("err = %v, want ErrParamsTooLarge", err)
	}
}

func TestCreateReusesAnIdenticalRecentJob(t *testing.T) {
	service, repo, publisher, _ := buildService(t, okRenderer())
	repo.reusable = &report.Job{ID: "job-existing", WorkspaceID: "ws-1", Status: report.StatusRunning}

	job := createJob(t, service)

	if job.ID != "job-existing" {
		t.Fatalf("job = %q, want the existing one", job.ID)
	}
	if len(publisher.published) != 0 {
		t.Fatal("a reused job must not be queued a second time")
	}
}

func TestCreateFailsTheJobWhenPublishingFails(t *testing.T) {
	service, repo, publisher, _ := buildService(t, okRenderer())
	publisher.err = errors.New("broker down")

	if _, err := service.Create(CreateInput{
		WorkspaceID: "ws-1",
		Kind:        report.KindAttendanceOverview,
		Format:      report.FormatCSV,
	}); err == nil {
		t.Fatal("a publish failure must surface to the caller")
	}

	if len(repo.created) != 1 {
		t.Fatalf("created %d jobs", len(repo.created))
	}
	stored := repo.jobs[repo.created[0].ID]
	if stored.Status != report.StatusFailed {
		t.Fatalf("status = %q, want %q: a job nothing will consume must not read as queued", stored.Status, report.StatusFailed)
	}
}

func TestGetRefusesAnotherWorkspace(t *testing.T) {
	service, _, _, _ := buildService(t, okRenderer())
	job := createJob(t, service)

	if _, err := service.Get("ws-2", job.ID); !errors.Is(err, report.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden: a job id is not an authorisation", err)
	}
}

func TestFileIsNotServedWhileTheJobIsNotDone(t *testing.T) {
	service, _, _, _ := buildService(t, okRenderer())
	job := createJob(t, service)

	if _, err := service.File(context.Background(), "ws-1", job.ID); !errors.Is(err, report.ErrNotReady) {
		t.Fatalf("err = %v, want ErrNotReady", err)
	}
}

func TestFileReportsAnExpiredJobAsExpired(t *testing.T) {
	service, repo, _, storage := buildService(t, okRenderer())
	job := createJob(t, service)

	at, _ := time.Parse(time.RFC3339, fixedNow)
	past := at.Add(-time.Hour)
	if err := storage.Upload("reports/ws-1/"+job.ID+".csv", []byte("x"), "text/csv"); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if err := repo.MarkDone(job.ID, "reports/ws-1/"+job.ID+".csv", "a.csv", 1, 1, at, &past); err != nil {
		t.Fatalf("mark done: %v", err)
	}

	if _, err := service.File(context.Background(), "ws-1", job.ID); !errors.Is(err, report.ErrExpired) {
		t.Fatalf("err = %v, want ErrExpired", err)
	}
}

func TestWorkerRendersUploadsAndCompletesTheJob(t *testing.T) {
	renderer := okRenderer()
	renderer.progress = []int{10, 70, 95}
	service, repo, publisher, storage := buildService(t, renderer)
	job := createJob(t, service)

	worker := NewWorker(service, nil)
	ack := &fakeAck{deliveries: 1}
	worker.handle(publisher.published[0], ack)

	if !ack.acked {
		t.Fatal("a successful render must ack the message")
	}
	stored := repo.jobs[job.ID]
	if stored.Status != report.StatusDone {
		t.Fatalf("status = %q, want %q", stored.Status, report.StatusDone)
	}
	if stored.ObjectKey == "" || stored.Filename != "atendimento.csv" {
		t.Fatalf("job = %+v", stored)
	}
	if stored.RowCount != 1 || stored.SizeBytes != int64(len(renderer.artifact.Data)) {
		t.Fatalf("job size and rows = %d, %d", stored.SizeBytes, stored.RowCount)
	}
	if _, found := storage.objects[stored.ObjectKey]; !found {
		t.Fatal("the file was never uploaded")
	}

	file, err := service.File(context.Background(), "ws-1", job.ID)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if string(file.Data) != string(renderer.artifact.Data) {
		t.Fatalf("file bytes = %q", string(file.Data))
	}
}

func TestWorkerNeverMarksDoneWhenTheUploadFails(t *testing.T) {
	service, repo, publisher, storage := buildService(t, okRenderer())
	job := createJob(t, service)
	storage.uploaErr = errors.New("bucket unreachable")

	worker := NewWorker(service, nil)
	worker.handle(publisher.published[0], &fakeAck{deliveries: messaging.MaxRetries})

	stored := repo.jobs[job.ID]
	if stored.Status == report.StatusDone {
		t.Fatal("a job whose file never landed must not read as done")
	}
	if stored.Status != report.StatusFailed {
		t.Fatalf("status = %q, want %q", stored.Status, report.StatusFailed)
	}
}

func TestWorkerRetriesBeforeGivingUp(t *testing.T) {
	renderer := okRenderer()
	renderer.err = errors.New("the source is down")
	service, repo, publisher, _ := buildService(t, renderer)
	job := createJob(t, service)

	worker := NewWorker(service, nil)
	ack := &fakeAck{deliveries: 1}
	worker.handle(publisher.published[0], ack)

	if !ack.nacked || !ack.requeued {
		t.Fatal("an early failure must be requeued")
	}
	if repo.jobs[job.ID].Status == report.StatusFailed {
		t.Fatal("a job must not be marked failed while retries remain")
	}

	final := &fakeAck{deliveries: messaging.MaxRetries}
	worker.handle(publisher.published[0], final)

	if final.requeued {
		t.Fatal("the last attempt must not be requeued")
	}
	if repo.jobs[job.ID].Status != report.StatusFailed {
		t.Fatalf("status = %q, want %q", repo.jobs[job.ID].Status, report.StatusFailed)
	}
}

func TestWorkerReportsARowCeilingWithItsOwnCode(t *testing.T) {
	renderer := okRenderer()
	renderer.err = export.ErrTooManyRows
	service, repo, publisher, _ := buildService(t, renderer)
	job := createJob(t, service)

	worker := NewWorker(service, nil)
	worker.handle(publisher.published[0], &fakeAck{deliveries: messaging.MaxRetries})

	if got := repo.failCode[job.ID]; got != report.FailureTooManyRows {
		t.Fatalf("failure code = %q, want %q", got, report.FailureTooManyRows)
	}
}

func TestWorkerDiscardsAnUnreadableMessage(t *testing.T) {
	service, _, _, _ := buildService(t, okRenderer())

	worker := NewWorker(service, nil)
	ack := &fakeAck{deliveries: 1}
	worker.handle([]byte("{not json"), ack)

	if !ack.nacked || ack.requeued {
		t.Fatal("an unreadable message must be dropped, never requeued forever")
	}
}

func TestWorkerFailsAJobWithNoRenderer(t *testing.T) {
	service, repo, publisher, _ := buildService(t, okRenderer())
	job := createJob(t, service)

	service.registry = report.NewRegistry()

	worker := NewWorker(service, nil)
	worker.handle(publisher.published[0], &fakeAck{deliveries: 1})

	if got := repo.failCode[job.ID]; got != report.FailureNoRenderer {
		t.Fatalf("failure code = %q, want %q", got, report.FailureNoRenderer)
	}
}

func TestFingerprintIgnoresKeyOrderButNotValues(t *testing.T) {
	first := report.Fingerprint("ws-1", report.KindAttendanceOverview, report.FormatCSV, "pt",
		json.RawMessage(`{"a":1,"b":2}`))
	second := report.Fingerprint("ws-1", report.KindAttendanceOverview, report.FormatCSV, "pt",
		json.RawMessage(`{"b":2,"a":1}`))
	third := report.Fingerprint("ws-1", report.KindAttendanceOverview, report.FormatCSV, "pt",
		json.RawMessage(`{"a":1,"b":3}`))

	if first != second {
		t.Fatal("the same parameters in a different order must reuse the same job")
	}
	if first == third {
		t.Fatal("different parameters must not collide")
	}
}

func TestFingerprintSeparatesWorkspaces(t *testing.T) {
	mine := report.Fingerprint("ws-1", report.KindAttendanceOverview, report.FormatCSV, "pt", nil)
	theirs := report.Fingerprint("ws-2", report.KindAttendanceOverview, report.FormatCSV, "pt", nil)

	if mine == theirs {
		t.Fatal("two workspaces must never share a report file")
	}
}

func TestWorkerDoesNotRetryADeterministicFailure(t *testing.T) {
	renderer := okRenderer()
	renderer.err = report.ErrEmptyResult
	service, repo, publisher, _ := buildService(t, renderer)
	job := createJob(t, service)

	worker := NewWorker(service, nil)
	ack := &fakeAck{deliveries: 1}
	worker.handle(publisher.published[0], ack)

	if ack.requeued {
		t.Fatal("an empty result will be empty again; requeueing it only burns the queue")
	}
	if repo.jobs[job.ID].Status != report.StatusFailed {
		t.Fatalf("status = %q, want %q", repo.jobs[job.ID].Status, report.StatusFailed)
	}
	if got := repo.failCode[job.ID]; got != report.FailureEmptyResult {
		t.Fatalf("failure code = %q, want %q", got, report.FailureEmptyResult)
	}
	if renderer.calls != 1 {
		t.Fatalf("renderer ran %d times, want 1", renderer.calls)
	}
}

func TestWorkerRetriesATransientFailureOnce(t *testing.T) {
	renderer := okRenderer()
	renderer.err = errors.New("connection reset")
	service, _, publisher, _ := buildService(t, renderer)
	createJob(t, service)

	worker := NewWorker(service, nil)
	ack := &fakeAck{deliveries: 1}
	worker.handle(publisher.published[0], ack)

	if !ack.requeued {
		t.Fatal("a transient failure must be retried")
	}
}

func TestListFiltersByStatusAndScopesToTheWorkspace(t *testing.T) {
	service, repo, _, _ := buildService(t, okRenderer())
	first := createJob(t, service)
	second := createJob(t, service)

	at, _ := time.Parse(time.RFC3339, fixedNow)
	if err := repo.MarkFailed(second.ID, report.FailureRenderFailed, at); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	repo.jobs["intruder"] = &report.Job{
		ID: "intruder", WorkspaceID: "ws-2", Status: report.StatusQueued,
	}

	page, err := service.List(report.ListQuery{
		WorkspaceID: "ws-1",
		Statuses:    []report.Status{report.StatusQueued},
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("total = %d, want 1", page.Total)
	}
	if len(page.Jobs) != 1 || page.Jobs[0].ID != first.ID {
		t.Fatalf("jobs = %+v", page.Jobs)
	}
}

func TestListCapsTheRequestedPageSize(t *testing.T) {
	service, _, _, _ := buildService(t, okRenderer())

	page, err := service.List(report.ListQuery{WorkspaceID: "ws-1", Limit: 5000})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Jobs) > report.MaxListLimit {
		t.Fatalf("returned %d jobs, want at most %d", len(page.Jobs), report.MaxListLimit)
	}
}

func TestListReportsAnExpiredJobAsExpired(t *testing.T) {
	service, repo, _, _ := buildService(t, okRenderer())
	job := createJob(t, service)

	at, _ := time.Parse(time.RFC3339, fixedNow)
	past := at.Add(-time.Hour)
	if err := repo.MarkDone(job.ID, "k", "a.csv", 1, 1, at, &past); err != nil {
		t.Fatalf("mark done: %v", err)
	}

	page, err := service.List(report.ListQuery{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Jobs) != 1 || page.Jobs[0].Status != report.StatusExpired {
		t.Fatalf("jobs = %+v", page.Jobs)
	}
}
