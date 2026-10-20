package mercadolivre

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	mldomain "vozko/domain/mercadolivre"
)

type catalogFixture struct {
	accounts  *fakeAccounts
	contacts  *fakeContacts
	listings  *fakeListings
	items     *fakeItems
	images    *fakeImages
	questions *fakeQuestionRepo
	api       *fakeQuestionAPI
	users     *fakeUsers
	rules     *fakeRules
	claims    *fakeClaims
	memo      *fakeMemo
	metrics   *fakeMetrics
	publisher *fakePublisher
	tokens    *TokenSource

	catalog  *Listings
	asked    *Questions
	account  *mldomain.Account
	creds    mldomain.Credentials
	services Services
}

func itemIDs(from, n int) []string {
	out := make([]string, 0, n)
	for i := from; i < from+n; i++ {
		out = append(out, "MLB"+strconv.Itoa(1000+i))
	}
	return out
}

func newCatalogFixture(t *testing.T) *catalogFixture {
	t.Helper()
	f := &catalogFixture{
		accounts:  newFakeAccounts(),
		contacts:  newFakeContacts(),
		listings:  newFakeListings(),
		items:     &fakeItems{items: map[string]*mldomain.RemoteItem{}},
		images:    &fakeImages{},
		questions: newFakeQuestionRepo(),
		api:       &fakeQuestionAPI{questions: map[string]*mldomain.RemoteQuestion{}},
		users:     &fakeUsers{users: map[string]*mldomain.User{buyerID: {ID: buyerID, Nickname: "COMPRADOR"}}},
		rules:     &fakeRules{},
		claims:    &fakeClaims{},
		memo:      &fakeMemo{},
		metrics:   newFakeMetrics(),
		publisher: newFakePublisher(),
	}
	f.account = f.accounts.put(connectedAccount())
	f.creds = f.account.Credentials()
	f.tokens = NewTokenSource(f.accounts, &fakeOAuth{refresh: rotated}, fakeClassifier{}, &fakeAlerts{}, f.metrics)
	f.tokens.now = fixedClock
	f.services = Services{Items: f.items, Questions: f.api, Users: f.users, Errors: fakeClassifier{}}
	f.catalog = NewListings(ListingsDeps{
		Accounts: f.accounts, Listings: f.listings, Services: f.services, Tokens: f.tokens, Images: f.images, Metrics: f.metrics,
	})
	f.catalog.now = fixedClock
	f.asked = NewQuestions(QuestionsDeps{
		Accounts: f.accounts, Contacts: f.contacts, Questions: f.questions, Listings: f.catalog, Services: f.services,
		Tokens: f.tokens, Rules: NewRuleEvaluator(f.rules, f.claims, f.metrics), Memo: f.memo, Metrics: f.metrics,
	})
	f.asked.now = fixedClock
	return f
}

func (f *catalogFixture) sellItems(ids ...string) {
	for _, id := range ids {
		f.items.items[id] = &mldomain.RemoteItem{ID: id, SellerID: sellerID, Title: "Item " + id, Status: "active", Thumbnail: "http://img/" + id + ".jpg"}
	}
}

func TestASyncPagesTheSellerItemsInBulkBatchesAndRehostsThumbnails(t *testing.T) {
	f := newCatalogFixture(t)
	first, second := itemIDs(0, 45), itemIDs(45, 5)
	f.items.pages = [][]string{first, second}
	f.sellItems(append(first, second...)...)
	f.items.items["MLB1003"].SellerID = "999"

	ctx := mldomain.WithBackgroundPriority(context.Background())
	synced, err := f.catalog.Reconcile(ctx, f.account, f.creds, "reconcile:1")
	if err != nil {
		t.Fatal(err)
	}
	if synced != 49 || len(f.listings.rows) != 49 {
		t.Fatalf("synced=%d rows=%d", synced, len(f.listings.rows))
	}
	if _, foreign := f.listings.rows[listingKey(f.account.ID, "MLB1003")]; foreign {
		t.Fatal("an item of another seller was mirrored")
	}
	for _, s := range f.items.searches {
		if !s.Scan || s.Limit != mldomain.MaxItemSearchLimit {
			t.Fatalf("search = %+v", s)
		}
	}
	for _, c := range f.items.ctxs {
		if !mldomain.IsBackground(c) {
			t.Fatal("a reconciliation sync must keep the background share")
		}
	}
	for _, b := range f.items.bulks {
		if len(b) > mldomain.MaxItemsPerBatch {
			t.Fatalf("bulk of %d", len(b))
		}
	}
	row := f.listings.rows[listingKey(f.account.ID, "MLB1000")]
	if row.ThumbnailKey != mldomain.ListingThumbnailKeyBase(f.account.ID, "MLB1000")+".jpg" || row.ThumbnailSource != "http://img/MLB1000.jpg" {
		t.Fatalf("thumbnail = %+v", row)
	}

	searches := len(f.items.searches)
	if again, err := f.catalog.Reconcile(ctx, f.account, f.creds, "reconcile:2"); err != nil || again != 0 || len(f.items.searches) != searches {
		t.Fatalf("a second sync inside 6 hours ran: %d %v", again, err)
	}
}

func TestAnUnchangedThumbnailIsNotDownloadedAgain(t *testing.T) {
	f := newCatalogFixture(t)
	f.items.pages = [][]string{{"MLB1000", "MLB1001"}}
	f.sellItems("MLB1000", "MLB1001")
	f.images.failFor = "http://img/MLB1001.jpg"
	if _, err := f.catalog.Reconcile(context.Background(), f.account, f.creds, "m"); err != nil {
		t.Fatal(err)
	}
	if f.metrics.count("listings/thumbnail_failed") != 1 || f.listings.rows[listingKey(f.account.ID, "MLB1001")] == nil {
		t.Fatalf("a failed thumbnail must keep the listing: %v", f.metrics.events)
	}
	f.images.stored = nil
	f.listings.claimedAt = map[string]time.Time{}
	f.images.failFor = ""
	if _, err := f.catalog.Reconcile(context.Background(), f.account, f.creds, "m2"); err != nil {
		t.Fatal(err)
	}
	if len(f.images.stored) != 1 {
		t.Fatalf("rehosted %v, want only the thumbnail that never stored", f.images.stored)
	}
}

func TestAFailedSyncReleasesItsClaim(t *testing.T) {
	f := newCatalogFixture(t)
	f.items.err = &mlErr{msg: "down", retryable: true}
	if _, err := f.catalog.Reconcile(context.Background(), f.account, f.creds, "m"); err == nil {
		t.Fatal("a failed search must fail the sync")
	}
	if f.listings.released != 1 {
		t.Fatal("a failed sync must be retried on the next reconciliation")
	}
	f.listings.claimErr = errors.New("db down")
	if _, err := f.catalog.Reconcile(context.Background(), f.account, f.creds, "m"); err == nil {
		t.Fatal("a claim that cannot be read refuses the sync")
	}
}

func TestAnItemNotificationRefreshesOneListing(t *testing.T) {
	f := newCatalogFixture(t)
	f.sellItems("MLB1000")
	job := mldomain.Job{Kind: mldomain.JobFetchItem, SellerID: sellerID, ResourceID: "MLB1000", Resource: "/items/MLB1000"}
	if err := f.catalog.Handle(context.Background(), f.account, job); err != nil {
		t.Fatal(err)
	}
	if row := f.listings.rows[listingKey(f.account.ID, "MLB1000")]; row == nil || row.ThumbnailKey == "" {
		t.Fatalf("listing = %+v", row)
	}

	f.items.items["MLB2000"] = &mldomain.RemoteItem{ID: "MLB2000", SellerID: "999"}
	if err := f.catalog.Handle(context.Background(), f.account, mldomain.Job{Kind: mldomain.JobFetchItem, ResourceID: "MLB2000"}); err != nil {
		t.Fatal(err)
	}
	if f.listings.rows[listingKey(f.account.ID, "MLB2000")] != nil || f.metrics.count("listings/not_owned") != 1 {
		t.Fatal("an item of another seller must be ignored and counted")
	}
	if err := f.catalog.Handle(context.Background(), f.account, mldomain.Job{Kind: mldomain.JobFetchItem, ResourceID: "MLB3000"}); !errors.Is(err, ErrResourceGone) {
		t.Fatalf("a missing item = %v", err)
	}
}

func TestListingsAreReadOnlyInsideTheirWorkspace(t *testing.T) {
	f := newCatalogFixture(t)
	f.sellItems("MLB1000")
	_ = f.catalog.Handle(context.Background(), f.account, mldomain.Job{Kind: mldomain.JobFetchItem, ResourceID: "MLB1000"})

	page, err := f.catalog.List(context.Background(), ListingsQuery{WorkspaceID: "ws-1", AccountID: f.account.ID})
	if err != nil || len(page.Items) != 1 || page.Items[0].ThumbnailURL == "" {
		t.Fatalf("page = %+v %v", page, err)
	}
	if _, err := f.catalog.List(context.Background(), ListingsQuery{WorkspaceID: "ws-2", AccountID: f.account.ID}); !errors.Is(err, mldomain.ErrAccountNotFound) {
		t.Fatalf("another workspace = %v", err)
	}
	if _, err := f.catalog.List(context.Background(), ListingsQuery{WorkspaceID: "ws-1", AccountID: f.account.ID, Cursor: "forged"}); err == nil {
		t.Fatal("a forged cursor must be refused")
	}
	url, err := f.catalog.ThumbnailURL(context.Background(), "ws-1", f.account.ID, "MLB1000")
	if err != nil || url != "https://cdn.example/"+mldomain.ListingThumbnailKeyBase(f.account.ID, "MLB1000")+".jpg" {
		t.Fatalf("thumbnail url = %q %v", url, err)
	}
	if _, err := f.catalog.ThumbnailURL(context.Background(), "ws-1", f.account.ID, "MLB9999"); !errors.Is(err, mldomain.ErrListingNotFound) {
		t.Fatalf("unknown listing = %v", err)
	}
}
