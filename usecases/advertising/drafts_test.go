package advertising

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	ads "vozko/domain/advertising"
)

type memoryDrafts struct {
	byID map[string]*ads.SavedDraft
	next int
}

func (m *memoryDrafts) Create(_ context.Context, d *ads.SavedDraft) error {
	m.next++
	d.ID, d.Version, d.UpdatedAt = "d-"+strconv.Itoa(m.next), 1, testNow
	clone := *d
	m.byID[d.ID] = &clone
	return nil
}

func (m *memoryDrafts) Find(_ context.Context, ws, id string) (*ads.SavedDraft, error) {
	d, ok := m.byID[id]
	if !ok || d.WorkspaceID != ws {
		return nil, ads.ErrDraftNotFound
	}
	clone := *d
	return &clone, nil
}

func (m *memoryDrafts) Save(_ context.Context, d *ads.SavedDraft) error {
	stored, ok := m.byID[d.ID]
	if !ok {
		return ads.ErrDraftNotFound
	}
	if stored.Version != d.Version {
		return ads.ErrDraftChanged
	}
	d.Version++
	d.JobID = ""
	clone := *d
	m.byID[d.ID] = &clone
	return nil
}

func (m *memoryDrafts) ClaimForPublish(_ context.Context, d *ads.SavedDraft, jobID string) error {
	stored := m.byID[d.ID]
	if stored.Version != d.Version {
		return ads.ErrDraftChanged
	}
	stored.Version++
	stored.JobID, stored.UpdatedAt = jobID, testNow
	d.Version, d.JobID = stored.Version, jobID
	return nil
}

func (m *memoryDrafts) ReleaseJob(_ context.Context, _, id, jobID string) error {
	if d := m.byID[id]; d != nil && d.JobID == jobID {
		d.JobID = ""
	}
	return nil
}

func (m *memoryDrafts) Delete(_ context.Context, d *ads.SavedDraft) error {
	stored, ok := m.byID[d.ID]
	if !ok {
		return ads.ErrDraftNotFound
	}
	if stored.Version != d.Version {
		return ads.ErrDraftChanged
	}
	delete(m.byID, d.ID)
	return nil
}

func (m *memoryDrafts) ListByAccount(_ context.Context, ws, accountID string) ([]*ads.SavedDraft, error) {
	var out []*ads.SavedDraft
	for _, d := range m.byID {
		if d.WorkspaceID == ws && d.AdAccountID == accountID {
			clone := *d
			out = append(out, &clone)
		}
	}
	return out, nil
}

type scriptedPublisher struct {
	jobs   *fakeJobs
	status ads.JobStatus
	err    error
	calls  int
	actor  ads.Actor
}

func (p *scriptedPublisher) Publish(ctx context.Context, in PublishInput) (*ads.PublishJob, error) {
	p.calls++
	p.actor = in.Actor
	if p.status == "" {
		return nil, p.err
	}
	job := &ads.PublishJob{ID: in.JobID, WorkspaceID: in.WorkspaceID, Draft: in.Draft, Status: p.status}
	_ = p.jobs.Create(ctx, job)
	return job, p.err
}

type draftsFixture struct {
	uc        *DraftsUseCase
	publisher *scriptedPublisher
}

func newDraftsFixture() draftsFixture {
	accounts := &fakeAccounts{byID: map[string]*ads.AdAccount{
		"acc-1": {ID: "acc-1", WorkspaceID: "ws"},
		"acc-x": {ID: "acc-x", WorkspaceID: "other"},
	}, connections: map[string]ads.Connection{}}
	jobs := &fakeJobs{byID: map[string]*ads.PublishJob{}}
	publisher := &scriptedPublisher{jobs: jobs}
	uc := NewDraftsUseCase(&memoryDrafts{byID: map[string]*ads.SavedDraft{}}, accounts, jobs, publisher)
	uc.now = func() time.Time { return testNow }
	ids := 0
	uc.newJobID = func() string {
		ids++
		return "job-" + strconv.Itoa(ids)
	}
	return draftsFixture{uc: uc, publisher: publisher}
}

func leadsTree(account string) ads.AdDraft {
	return ads.AdDraft{
		AdAccountID: account,
		Campaign:    ads.CampaignDraft{Name: "Nova campanha de Leads", Objective: ads.ObjectiveLeads},
		AdSet:       ads.AdSetDraft{Name: "Novo conjunto"},
		Ads:         []ads.AdItem{{Name: "Novo anúncio"}},
	}
}

func TestDraftsBelongToAnAccountOfTheWorkspace(t *testing.T) {
	f := newDraftsFixture()
	if _, err := f.uc.Create(context.Background(), "ws", "u-1", leadsTree("acc-x")); !errors.Is(err, ads.ErrAccountNotFound) {
		t.Fatalf("a foreign account must be refused, got %v", err)
	}
	if _, err := f.uc.List(context.Background(), "ws", "acc-x"); !errors.Is(err, ads.ErrAccountNotFound) {
		t.Fatalf("listing a foreign account must be refused, got %v", err)
	}
}

func TestDraftsListCountsTheirObjects(t *testing.T) {
	f := newDraftsFixture()
	ctx := context.Background()
	if _, err := f.uc.Create(ctx, "ws", "u-1", leadsTree("acc-1")); err != nil {
		t.Fatal(err)
	}
	list, err := f.uc.List(ctx, "ws", "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Drafts) != 1 || list.ObjectCount != 3 || list.Drafts[0].State != ads.DraftEditing {
		t.Fatalf("got %+v", list)
	}
}

func TestDraftUpdateNeedsTheCurrentVersion(t *testing.T) {
	f := newDraftsFixture()
	ctx := context.Background()
	created, _ := f.uc.Create(ctx, "ws", "u-1", leadsTree("acc-1"))
	changed := leadsTree("acc-1")
	changed.Campaign.Name = "Black Friday"
	updated, err := f.uc.Update(ctx, "ws", "u-2", created.Draft.ID, 1, changed)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Draft.Version != 2 || updated.Draft.Content.Campaign.Name != "Black Friday" || updated.Draft.UpdatedBy != "u-2" {
		t.Fatalf("got %+v", updated.Draft)
	}
	if _, err := f.uc.Update(ctx, "ws", "u-1", created.Draft.ID, 1, changed); !errors.Is(err, ads.ErrDraftChanged) {
		t.Fatalf("a stale version must be refused, got %v", err)
	}
}

func TestPublishingADraftRemovesItOncePublished(t *testing.T) {
	f := newDraftsFixture()
	ctx := context.Background()
	created, _ := f.uc.Create(ctx, "ws", "u-1", leadsTree("acc-1"))
	f.publisher.status = ads.JobPublished
	job, err := f.uc.Publish(ctx, "ws", "u-1", created.Draft.ID, 1, ads.ActorPerson)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "job-1" || job.Draft.Campaign.Name != "Nova campanha de Leads" {
		t.Fatalf("got %+v", job)
	}
	if _, err := f.uc.Get(ctx, "ws", created.Draft.ID); !errors.Is(err, ads.ErrDraftNotFound) {
		t.Fatalf("the published draft must be gone, got %v", err)
	}
}

func TestADraftBeingPublishedCannotChangeOrPublishTwice(t *testing.T) {
	f := newDraftsFixture()
	ctx := context.Background()
	created, _ := f.uc.Create(ctx, "ws", "u-1", leadsTree("acc-1"))
	f.publisher.status = ads.JobRunning
	if _, err := f.uc.Publish(ctx, "ws", "u-1", created.Draft.ID, 1, ads.ActorPerson); err != nil {
		t.Fatal(err)
	}
	if _, err := f.uc.Publish(ctx, "ws", "u-1", created.Draft.ID, 1, ads.ActorPerson); !errors.Is(err, ads.ErrDraftPublishing) {
		t.Fatalf("got %v", err)
	}
	if _, err := f.uc.Update(ctx, "ws", "u-1", created.Draft.ID, 2, leadsTree("acc-1")); !errors.Is(err, ads.ErrDraftPublishing) {
		t.Fatalf("got %v", err)
	}
	if err := f.uc.Delete(ctx, "ws", created.Draft.ID); !errors.Is(err, ads.ErrDraftPublishing) {
		t.Fatalf("got %v", err)
	}
	if f.publisher.calls != 1 {
		t.Fatalf("published %d times", f.publisher.calls)
	}
}

func TestAFailedPublishLeavesTheDraftEditable(t *testing.T) {
	f := newDraftsFixture()
	ctx := context.Background()
	created, _ := f.uc.Create(ctx, "ws", "u-1", leadsTree("acc-1"))
	f.publisher.status = ads.JobFailed
	if _, err := f.uc.Publish(ctx, "ws", "u-1", created.Draft.ID, 1, ads.ActorPerson); err != nil {
		t.Fatal(err)
	}
	view, _ := f.uc.Get(ctx, "ws", created.Draft.ID)
	if view.State != ads.DraftFailed || view.Job == nil {
		t.Fatalf("got %+v", view)
	}
	updated, err := f.uc.Update(ctx, "ws", "u-1", created.Draft.ID, view.Draft.Version, leadsTree("acc-1"))
	if err != nil || updated.State != ads.DraftEditing {
		t.Fatalf("got %+v %v", updated, err)
	}
}

func TestAPublishRefusedBeforeAnyJobReleasesTheDraft(t *testing.T) {
	f := newDraftsFixture()
	ctx := context.Background()
	created, _ := f.uc.Create(ctx, "ws", "u-1", leadsTree("acc-1"))
	refused := ads.FieldError("adSet.budget", "required")
	f.publisher.err = refused
	if _, err := f.uc.Publish(ctx, "ws", "u-1", created.Draft.ID, 1, ads.ActorPerson); !errors.Is(err, refused) {
		t.Fatalf("got %v", err)
	}
	view, _ := f.uc.Get(ctx, "ws", created.Draft.ID)
	if view.State != ads.DraftEditing || view.Draft.JobID != "" {
		t.Fatalf("got %+v", view)
	}
}

func TestDiscardSkipsDraftsBeingPublished(t *testing.T) {
	f := newDraftsFixture()
	ctx := context.Background()
	first, _ := f.uc.Create(ctx, "ws", "u-1", leadsTree("acc-1"))
	if _, err := f.uc.Create(ctx, "ws", "u-1", leadsTree("acc-1")); err != nil {
		t.Fatal(err)
	}
	f.publisher.status = ads.JobRunning
	if _, err := f.uc.Publish(ctx, "ws", "u-1", first.Draft.ID, 1, ads.ActorPerson); err != nil {
		t.Fatal(err)
	}
	discarded, err := f.uc.Discard(ctx, "ws", "acc-1")
	if err != nil || discarded != 1 {
		t.Fatalf("discarded %d, %v", discarded, err)
	}
	if _, err := f.uc.Get(ctx, "ws", first.Draft.ID); err != nil {
		t.Fatalf("the publishing draft must stay, got %v", err)
	}
}

func TestDuplicatingADraftCreatesAnIndependentCopy(t *testing.T) {
	f := newDraftsFixture()
	ctx := context.Background()
	created, _ := f.uc.Create(ctx, "ws", "u-1", leadsTree("acc-1"))
	copied, err := f.uc.Duplicate(ctx, "ws", "u-2", created.Draft.ID, "Nova campanha de Leads - Cópia")
	if err != nil {
		t.Fatal(err)
	}
	if copied.Draft.ID == created.Draft.ID || copied.Draft.Content.Campaign.Name != "Nova campanha de Leads - Cópia" {
		t.Fatalf("got %+v", copied.Draft)
	}
	list, _ := f.uc.List(ctx, "ws", "acc-1")
	if len(list.Drafts) != 2 {
		t.Fatalf("got %d drafts", len(list.Drafts))
	}
}

func TestPublishingNeedsTheVersionThatWasReviewed(t *testing.T) {
	f := newDraftsFixture()
	ctx := context.Background()
	created, _ := f.uc.Create(ctx, "ws", "u-1", leadsTree("acc-1"))
	if _, err := f.uc.Update(ctx, "ws", "u-2", created.Draft.ID, 1, leadsTree("acc-1")); err != nil {
		t.Fatal(err)
	}
	f.publisher.status = ads.JobPublished
	if _, err := f.uc.Publish(ctx, "ws", "u-1", created.Draft.ID, 1, ads.ActorPerson); !errors.Is(err, ads.ErrDraftChanged) {
		t.Fatalf("got %v", err)
	}
	if f.publisher.calls != 0 {
		t.Fatal("nothing may be published")
	}
}

func TestPublishingRecordsWhoAskedForIt(t *testing.T) {
	f := newDraftsFixture()
	ctx := context.Background()
	created, _ := f.uc.Create(ctx, "ws", "u-1", leadsTree("acc-1"))
	f.publisher.status = ads.JobPublished
	if _, err := f.uc.Publish(ctx, "ws", "u-1", created.Draft.ID, 1, ads.ActorAssistant); err != nil {
		t.Fatal(err)
	}
	if f.publisher.actor != ads.ActorAssistant {
		t.Fatalf("actor %q", f.publisher.actor)
	}
}

func TestCheckEditRefusesAStaleVersionOrADraftBeingPublished(t *testing.T) {
	f := newDraftsFixture()
	ctx := context.Background()
	created, _ := f.uc.Create(ctx, "ws", "u-1", leadsTree("acc-1"))
	if view, err := f.uc.CheckEdit(ctx, "ws", created.Draft.ID, 1); err != nil || view.Draft.ID != created.Draft.ID {
		t.Fatalf("got %+v %v", view, err)
	}
	if _, err := f.uc.CheckEdit(ctx, "ws", created.Draft.ID, 7); !errors.Is(err, ads.ErrDraftChanged) {
		t.Fatalf("a stale version must be refused, got %v", err)
	}
	f.publisher.status = ads.JobRunning
	if _, err := f.uc.Publish(ctx, "ws", "u-1", created.Draft.ID, 1, ads.ActorPerson); err != nil {
		t.Fatal(err)
	}
	if _, err := f.uc.CheckEdit(ctx, "ws", created.Draft.ID, 2); !errors.Is(err, ads.ErrDraftPublishing) {
		t.Fatalf("a draft being published must be refused, got %v", err)
	}
}
