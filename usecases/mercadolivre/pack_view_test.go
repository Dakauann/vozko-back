package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/webhook"
)

type packViewFixture struct {
	packs         *Packs
	accounts      *fakeAccounts
	conversations *fakeConversations
	listings      *fakeListings
	queue         *fakePublisher
}

func newPackViewFixture(allowed bool) *packViewFixture {
	f := &packViewFixture{
		accounts:      newFakeAccounts(),
		conversations: newFakeConversations(),
		listings:      newFakeListings(),
		queue:         newFakePublisher(),
	}
	f.accounts.put(connectedAccount())
	refreshed := testNow.Add(-time.Hour)
	f.conversations.put(&mldomain.Conversation{ID: "conv-1", WorkspaceID: "ws-1", AccountID: "acct-1", PackKey: packKey,
		PackSummaryRefreshedAt: &refreshed,
		PackSummary: &mldomain.PackSummary{
			OrderStatus: "paid", ShippingStatus: "shipped", ShippingSubstatus: "out_for_delivery", TotalAmount: 59.8, CurrencyID: "BRL",
			Items: []mldomain.PackItem{
				{ItemID: "MLB1", Title: "Caneca", Quantity: 2, UnitPrice: 19.9},
				{ItemID: "MLB2", Title: "Prato", Quantity: 1, UnitPrice: 20, CurrencyID: "USD", ThumbnailKey: "packs/MLB2.jpg", Permalink: "https://produto.mercadolivre.com.br/MLB-2"},
				{ItemID: "MLB3", Title: "Copo", Quantity: 1, UnitPrice: 0},
			},
		}})
	f.listings.rows[listingKey("acct-1", "MLB1")] = &mldomain.Listing{AccountID: "acct-1", ItemID: "MLB1", ThumbnailKey: "listings/MLB1.jpg", Permalink: "https://produto.mercadolivre.com.br/MLB-1"}
	f.packs = NewPacks(PacksDeps{
		Accounts: f.accounts, Conversations: f.conversations, Listings: f.listings, Images: &fakeImages{},
		Refresh: NewJobPublisher(f.queue), Access: fakeAccess{allowed: allowed}, Metrics: newFakeMetrics(),
	})
	f.packs.now = fixedClock
	return f
}

func (f *packViewFixture) queued(t *testing.T) []mldomain.Job {
	t.Helper()
	var jobs []mldomain.Job
	for _, raw := range f.queue.published[webhook.TopicMercadoLivreOrder] {
		var job mldomain.Job
		if err := json.Unmarshal(raw, &job); err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, job)
	}
	return jobs
}

func TestThePackSummaryIsServedWithStoredThumbnailsOnly(t *testing.T) {
	f := newPackViewFixture(true)
	view, err := f.packs.Summary(context.Background(), operatorQuery())
	if err != nil {
		t.Fatal(err)
	}
	if view.OrderStatus != "paid" || view.ShippingStatus != "shipped" || view.ShippingSubstatus != "out_for_delivery" ||
		view.TotalAmount != 59.8 || view.CurrencyID != "BRL" || !view.RefreshedAt.Equal(testNow.Add(-time.Hour)) || len(view.Items) != 3 {
		t.Fatalf("view = %+v", view)
	}
	mirrored, rehosted, bare := view.Items[0], view.Items[1], view.Items[2]
	if mirrored.ThumbnailURL != "https://cdn.example/listings/MLB1.jpg" || mirrored.Permalink != "https://produto.mercadolivre.com.br/MLB-1" || mirrored.CurrencyID != "BRL" {
		t.Fatalf("an item in the listing mirror = %+v", mirrored)
	}
	if rehosted.ThumbnailURL != "https://cdn.example/packs/MLB2.jpg" || rehosted.CurrencyID != "USD" {
		t.Fatalf("an item re-hosted with the summary = %+v", rehosted)
	}
	if bare.ThumbnailURL != "" || bare.Permalink != "" {
		t.Fatalf("an item without a stored image = %+v", bare)
	}
	if len(f.queued(t)) != 0 {
		t.Fatal("a fresh summary queues nothing")
	}
}

func TestAStalePackSummaryIsServedAndQueuesOneRefresh(t *testing.T) {
	f := newPackViewFixture(true)
	old := testNow.Add(-25 * time.Hour)
	conv := f.conversations.get("conv-1")
	conv.PackSummaryRefreshedAt = &old
	f.conversations.put(conv)

	for i := 0; i < 3; i++ {
		view, err := f.packs.Summary(context.Background(), operatorQuery())
		if err != nil || len(view.Items) != 3 {
			t.Fatalf("the stored summary stands until the refresh lands: %+v %v", view, err)
		}
	}
	jobs := f.queued(t)
	if len(jobs) != 3 {
		t.Fatalf("queued = %d", len(jobs))
	}
	keys := map[string]bool{}
	for _, job := range jobs {
		if job.Kind != mldomain.JobRefreshPack || job.ResourceID != packKey || job.SellerID != sellerID {
			t.Fatalf("job = %+v", job)
		}
		keys[job.IdempotencyKey()] = true
	}
	if len(keys) != 1 {
		t.Fatalf("repeated reads of one stale summary must share one dedup key: %v", keys)
	}
}

func TestAMissingPackSummaryQueuesARefreshAndSaysItIsPending(t *testing.T) {
	f := newPackViewFixture(true)
	conv := f.conversations.get("conv-1")
	conv.PackSummary, conv.PackSummaryRefreshedAt = nil, nil
	f.conversations.put(conv)

	if _, err := f.packs.Summary(context.Background(), operatorQuery()); !errors.Is(err, mldomain.ErrPackSummaryPending) {
		t.Fatalf("err = %v", err)
	}
	if len(f.queued(t)) != 1 {
		t.Fatal("a missing summary must be loaded in the background")
	}
}

func TestAnAccountThatCannotReadQueuesNoPackRefresh(t *testing.T) {
	f := newPackViewFixture(true)
	account := connectedAccount()
	account.Status = mldomain.StatusNeedsReconnect
	f.accounts.put(account)
	old := testNow.Add(-25 * time.Hour)
	conv := f.conversations.get("conv-1")
	conv.PackSummaryRefreshedAt = &old
	f.conversations.put(conv)

	if _, err := f.packs.Summary(context.Background(), operatorQuery()); err != nil {
		t.Fatal(err)
	}
	if len(f.queued(t)) != 0 {
		t.Fatal("a disconnected account must not queue reads it cannot make")
	}
}

func TestThePackSummaryUsesTheConversationGuard(t *testing.T) {
	bot := operatorQuery()
	bot.Actor = conversation.SentByAI("agent-1")
	if _, err := newPackViewFixture(true).packs.Summary(context.Background(), bot); !errors.Is(err, mldomain.ErrPersonOnly) {
		t.Fatalf("an automation reads no pack summary here, err = %v", err)
	}
	if _, err := newPackViewFixture(false).packs.Summary(context.Background(), operatorQuery()); !errors.Is(err, conversation.ErrUnauthorized) {
		t.Fatalf("an operator without access to the conversation, err = %v", err)
	}
	other := operatorQuery()
	other.WorkspaceID = "ws-2"
	if _, err := newPackViewFixture(true).packs.Summary(context.Background(), other); !errors.Is(err, mldomain.ErrConversationNotFound) {
		t.Fatalf("another workspace, err = %v", err)
	}
}
