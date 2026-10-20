package mercadolivre

import (
	"context"
	"reflect"
	"testing"

	mldomain "vozko/domain/mercadolivre"
)

type packSummaryFixture struct {
	summaries     *PackSummaries
	conversations *fakeConversations
	listings      *fakeListings
	items         *fakeItems
	images        *fakeImages
	metrics       *fakeMetrics
	sales         *fakeSales
}

func newPackSummaryFixture() *packSummaryFixture {
	order := &mldomain.RemoteOrder{ID: "2000000000000001", PackID: packKey, Status: "paid", CurrencyID: "BRL", TotalAmount: 40, Items: []mldomain.RemoteOrderItem{
		{ItemID: "MLB1", Title: "Caneca", Quantity: 1, UnitPrice: 10},
		{ItemID: "MLB2", Title: "Prato", Quantity: 1, UnitPrice: 10},
		{ItemID: "MLB3", Title: "Copo", Quantity: 1, UnitPrice: 10},
		{ItemID: "MLB4", Title: "Garfo", Quantity: 1, UnitPrice: 10},
	}}
	f := &packSummaryFixture{
		conversations: newFakeConversations(),
		listings:      newFakeListings(),
		items: &fakeItems{items: map[string]*mldomain.RemoteItem{
			"MLB2": {ID: "MLB2", SellerID: sellerID, Thumbnail: "https://http2.mlstatic.com/prato.jpg", Permalink: "https://produto.mercadolivre.com.br/MLB-2"},
			"MLB4": {ID: "MLB4", SellerID: sellerID, Thumbnail: "https://http2.mlstatic.com/garfo.jpg", Permalink: "https://produto.mercadolivre.com.br/MLB-4"},
		}},
		images:  &fakeImages{},
		metrics: newFakeMetrics(),
		sales: &fakeSales{
			packs:  map[string]*mldomain.RemotePack{packKey: {ID: packKey, OrderIDs: []string{order.ID}}},
			orders: map[string]*mldomain.RemoteOrder{order.ID: order},
		},
	}
	f.listings.rows[listingKey("acct-1", "MLB1")] = &mldomain.Listing{AccountID: "acct-1", ItemID: "MLB1", ThumbnailKey: "mercadolivre/accounts/acct-1/listings/MLB1/thumbnail.jpg"}
	f.listings.rows[listingKey("acct-1", "MLB4")] = &mldomain.Listing{AccountID: "acct-1", ItemID: "MLB4"}
	f.conversations.put(&mldomain.Conversation{ID: "conv-1", WorkspaceID: "ws-1", AccountID: "acct-1", PackKey: packKey,
		PackSummary: &mldomain.PackSummary{Items: []mldomain.PackItem{{ItemID: "MLB3", ThumbnailKey: "kept/MLB3.jpg", Permalink: "https://produto.mercadolivre.com.br/MLB-3"}}}})
	f.summaries = NewPackSummaries(PackSummariesDeps{
		Conversations: f.conversations,
		Listings:      f.listings,
		Services:      Services{Packs: f.sales, Orders: f.sales, Shipments: f.sales, Items: f.items, Errors: fakeClassifier{}},
		Images:        f.images,
		Metrics:       f.metrics,
	})
	f.summaries.now = fixedClock
	return f
}

func (f *packSummaryFixture) refresh(t *testing.T) map[string]mldomain.PackItem {
	t.Helper()
	if err := f.summaries.Refresh(context.Background(), mldomain.Credentials{}, f.conversations.get("conv-1")); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	stored := f.conversations.get("conv-1").PackSummary
	byItem := map[string]mldomain.PackItem{}
	for _, item := range stored.Items {
		byItem[item.ItemID] = item
	}
	return byItem
}

func TestOnlyPackItemsOutsideTheListingMirrorAreRehosted(t *testing.T) {
	f := newPackSummaryFixture()
	items := f.refresh(t)

	if !reflect.DeepEqual(f.items.bulks, [][]string{{"MLB2", "MLB4"}}) {
		t.Fatalf("item reads = %v, want one bulk read of the items without a stored thumbnail", f.items.bulks)
	}
	if items["MLB1"].ThumbnailKey != "" {
		t.Fatalf("an item in the listing mirror is served from the mirror, got %+v", items["MLB1"])
	}
	for _, id := range []string{"MLB2", "MLB4"} {
		want := mldomain.PackItemThumbnailKeyBase("acct-1", "conv-1", id) + ".jpg"
		if items[id].ThumbnailKey != want || items[id].Permalink == "" {
			t.Fatalf("%s = %+v, want key %s and the permalink", id, items[id], want)
		}
		if f.images.stored[want] == "" {
			t.Fatalf("%s was not re-hosted: %v", id, f.images.stored)
		}
	}
	if items["MLB3"].ThumbnailKey != "kept/MLB3.jpg" || items["MLB3"].Permalink != "https://produto.mercadolivre.com.br/MLB-3" {
		t.Fatalf("a thumbnail stored before is kept without a new download, got %+v", items["MLB3"])
	}
}

func TestAThumbnailThatCannotBeRehostedLeavesTheSummaryWithoutIt(t *testing.T) {
	f := newPackSummaryFixture()
	f.images.failFor = "https://http2.mlstatic.com/prato.jpg"
	items := f.refresh(t)

	if items["MLB2"].ThumbnailKey != "" || items["MLB2"].Title != "Prato" {
		t.Fatalf("MLB2 = %+v, want the item without a thumbnail", items["MLB2"])
	}
	if items["MLB4"].ThumbnailKey == "" {
		t.Fatal("one failed thumbnail must not stop the others")
	}
	if f.metrics.count("pack_summary/thumbnail_failed") != 1 {
		t.Fatalf("metrics = %v", f.metrics.events)
	}
}

func TestAFailedItemReadStillStoresTheSummary(t *testing.T) {
	f := newPackSummaryFixture()
	f.items.err = &mlErr{msg: "items down"}
	items := f.refresh(t)
	if len(items) != 4 || items["MLB2"].ThumbnailKey != "" {
		t.Fatalf("items = %+v", items)
	}
	if f.metrics.count("pack_summary/thumbnail_failed") != 1 {
		t.Fatalf("metrics = %v", f.metrics.events)
	}
}
