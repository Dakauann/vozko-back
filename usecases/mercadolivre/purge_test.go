package mercadolivre

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
)

type purgeAccounts struct {
	*fakeAccounts
	purged     []string
	reconnects map[string]bool
}

func (p *purgeAccounts) ListDueForPurge(_ context.Context, now time.Time, limit int) ([]*mldomain.Account, error) {
	var out []*mldomain.Account
	for _, a := range p.rows {
		if a.PurgeDue(now) && !contains(p.purged, a.ID) {
			out = append(out, cloneAccount(a))
		}
	}
	return out, nil
}

func (p *purgeAccounts) FindByWorkspaceAndMLUserIDUnscoped(_ context.Context, workspaceID, mlUserID string) (*mldomain.Account, error) {
	for _, a := range p.rows {
		if a.WorkspaceID == workspaceID && a.MLUserID == mlUserID {
			c := cloneAccount(a)
			if p.reconnects[a.ID] {
				c.Status, c.PurgeAfter = mldomain.StatusConnected, nil
			}
			return c, nil
		}
	}
	return nil, mldomain.ErrAccountNotFound
}

func (p *purgeAccounts) DeletePurged(_ context.Context, id string) error {
	p.purged = append(p.purged, id)
	return nil
}

func contains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

type fakePurgeRepo struct {
	conversations  []string
	packThumbnails map[string][]string
	questions      int
	listings       []mldomain.ListingThumbnail
	contacts       []string
	syncDeleted    bool
	steps          []string
	failListings   error
}

func take(values []string, n int) []string { return values[:min(n, len(values))] }

func drop(values, gone []string) []string {
	var out []string
	for _, v := range values {
		if !contains(gone, v) {
			out = append(out, v)
		}
	}
	return out
}

func (f *fakePurgeRepo) ConversationIDs(_ context.Context, _ string, limit int) ([]string, error) {
	return take(f.conversations, limit), nil
}

func (f *fakePurgeRepo) PackThumbnailKeys(_ context.Context, _ string, ids []string) ([]string, error) {
	var keys []string
	for _, id := range ids {
		keys = append(keys, f.packThumbnails[id]...)
	}
	return keys, nil
}

func (f *fakePurgeRepo) DeleteConversations(_ context.Context, _ string, ids []string) error {
	f.steps = append(f.steps, "conversations:"+strings.Join(ids, ","))
	f.conversations = drop(f.conversations, ids)
	return nil
}

func (f *fakePurgeRepo) DeleteQuestions(_ context.Context, _ string, limit int) (int64, error) {
	n := min(limit, f.questions)
	f.questions -= n
	if n > 0 {
		f.steps = append(f.steps, fmt.Sprintf("questions:%d", n))
	}
	return int64(n), nil
}

func (f *fakePurgeRepo) ListingThumbnails(_ context.Context, _ string, limit int) ([]mldomain.ListingThumbnail, error) {
	if f.failListings != nil {
		return nil, f.failListings
	}
	return f.listings[:min(limit, len(f.listings))], nil
}

func (f *fakePurgeRepo) DeleteListings(_ context.Context, _ string, items []string) error {
	f.steps = append(f.steps, "listings:"+strings.Join(items, ","))
	var rest []mldomain.ListingThumbnail
	for _, l := range f.listings {
		if !contains(items, l.ItemID) {
			rest = append(rest, l)
		}
	}
	f.listings = rest
	return nil
}

func (f *fakePurgeRepo) DeleteListingSync(context.Context, string) error {
	f.syncDeleted = true
	return nil
}

func (f *fakePurgeRepo) ContactIDs(_ context.Context, _ string, limit int) ([]string, error) {
	return take(f.contacts, limit), nil
}

func (f *fakePurgeRepo) DeleteContacts(_ context.Context, _ string, ids []string) error {
	f.steps = append(f.steps, "contacts:"+strings.Join(ids, ","))
	f.contacts = drop(f.contacts, ids)
	return nil
}

type fakeEntryPurger struct {
	repo     *fakePurgeRepo
	entries  [][]string
	contacts [][]string
	files    []string
	types    []shared.EntryType
}

func (f *fakeEntryPurger) PurgeEntries(_ context.Context, t shared.EntryType, ids []string) error {
	f.types = append(f.types, t)
	f.entries = append(f.entries, ids)
	f.repo.steps = append(f.repo.steps, "entries:"+strings.Join(ids, ","))
	return nil
}

func (f *fakeEntryPurger) PurgeContacts(_ context.Context, t shared.EntryType, ids []string) error {
	f.types = append(f.types, t)
	f.contacts = append(f.contacts, ids)
	f.repo.steps = append(f.repo.steps, "portfolio:"+strings.Join(ids, ","))
	return nil
}

func (f *fakeEntryPurger) DeleteFiles(_ context.Context, keys []string) error {
	f.files = append(f.files, keys...)
	f.repo.steps = append(f.repo.steps, "files:"+strings.Join(keys, ","))
	return nil
}

type fakeRulePurger struct{ purged []string }

func (f *fakeRulePurger) PurgeAccount(_ context.Context, source shared.EntryType, accountID string) error {
	f.purged = append(f.purged, string(source)+"|"+accountID)
	return nil
}

type purgeFixture struct {
	job      *PurgeJob
	accounts *purgeAccounts
	repo     *fakePurgeRepo
	entries  *fakeEntryPurger
	rules    *fakeRulePurger
	metrics  *fakeMetrics
}

func newPurgeFixture(purgeAfter time.Time) *purgeFixture {
	f := &purgeFixture{
		accounts: &purgeAccounts{fakeAccounts: newFakeAccounts(), reconnects: map[string]bool{}},
		repo: &fakePurgeRepo{
			conversations:  []string{"c1", "c2", "c3"},
			packThumbnails: map[string][]string{"c1": {"p1.jpg"}, "c3": {"p3.jpg"}},
			questions:      3,
			listings:       []mldomain.ListingThumbnail{{ItemID: "MLB1", Key: "k1.jpg"}, {ItemID: "MLB2"}},
			contacts:       []string{"ct1", "ct2"},
		},
		rules:   &fakeRulePurger{},
		metrics: newFakeMetrics(),
	}
	f.entries = &fakeEntryPurger{repo: f.repo}
	acc := connectedAccount()
	acc.Status, acc.AccessToken, acc.RefreshToken, acc.PurgeAfter = mldomain.StatusDisconnected, "", "", &purgeAfter
	f.accounts.put(acc)
	f.job = NewPurgeJob(PurgeDeps{Accounts: f.accounts, Purge: f.repo, Entries: f.entries, Rules: f.rules, Metrics: f.metrics})
	f.job.now = fixedClock
	f.job.batch = 2
	return f
}

func TestADueAccountIsPurgedInBatchesFilesBeforeRowsAndTheAccountLast(t *testing.T) {
	f := newPurgeFixture(testNow.Add(-time.Hour))
	if err := f.job.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"entries:c1,c2", "files:p1.jpg", "conversations:c1,c2", "entries:c3", "files:p3.jpg", "conversations:c3",
		"questions:2", "questions:1",
		"files:k1.jpg", "listings:MLB1,MLB2",
		"portfolio:ct1,ct2", "contacts:ct1,ct2",
	}
	if strings.Join(f.repo.steps, " ") != strings.Join(want, " ") {
		t.Fatalf("steps =\n%v\nwant\n%v", f.repo.steps, want)
	}
	for _, typ := range f.entries.types {
		if typ != shared.EntryTypeMercadoLivre {
			t.Fatalf("purged type %q", typ)
		}
	}
	if !f.repo.syncDeleted || joined(f.rules.purged) != "mercadolivre|acct-1" || joined(f.accounts.purged) != "acct-1" {
		t.Fatalf("sync=%v rules=%v account=%v", f.repo.syncDeleted, f.rules.purged, f.accounts.purged)
	}
	if f.metrics.count("purge/account_purged") != 1 {
		t.Fatalf("metrics = %v", f.metrics.events)
	}
	if err := f.job.Execute(context.Background()); err != nil || len(f.accounts.purged) != 1 {
		t.Fatalf("a second run must find nothing to do: %v %v", err, f.accounts.purged)
	}
}

func TestAnAccountInsideItsGracePeriodIsKept(t *testing.T) {
	f := newPurgeFixture(testNow.Add(time.Hour))
	if err := f.job.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.repo.steps) != 0 || len(f.accounts.purged) != 0 {
		t.Fatalf("steps = %v", f.repo.steps)
	}
}

func TestAReconnectDuringThePurgeStopsIt(t *testing.T) {
	f := newPurgeFixture(testNow.Add(-time.Hour))
	f.accounts.reconnects["acct-1"] = true
	if err := f.job.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.repo.steps) != 0 || len(f.accounts.purged) != 0 || f.metrics.count("purge/cancelled") != 1 {
		t.Fatalf("steps=%v purged=%v metrics=%v", f.repo.steps, f.accounts.purged, f.metrics.events)
	}
}

func TestAFailedPurgeKeepsTheAccountForTheNextRun(t *testing.T) {
	f := newPurgeFixture(testNow.Add(-time.Hour))
	f.repo.failListings = errors.New("db down")
	if err := f.job.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.accounts.purged) != 0 || f.metrics.count("purge/failed") != 1 {
		t.Fatalf("purged=%v metrics=%v", f.accounts.purged, f.metrics.events)
	}
	f.repo.failListings = nil
	if err := f.job.Execute(context.Background()); err != nil || joined(f.accounts.purged) != "acct-1" {
		t.Fatalf("the next run must resume: %v %v", err, f.accounts.purged)
	}
}
