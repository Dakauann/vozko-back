package mercadolivre

import (
	"errors"
	"testing"
	"time"
)

func sellerAccount() *Account {
	return &Account{ID: "acc-1", WorkspaceID: "ws-1", MLUserID: "415458330", SiteID: SiteMLB, Status: StatusConnected}
}

func TestAListingIsMirroredOnlyFromItsOwnSeller(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	item := &RemoteItem{ID: "MLB123", SellerID: "415458330", Title: "  Caneca  ", Permalink: "https://produto", Thumbnail: "http://img/1.jpg", Status: "active"}
	got, err := ListingFromRemote(sellerAccount(), item, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "acc-1" || got.WorkspaceID != "ws-1" || got.ItemID != "MLB123" || got.Title != "Caneca" ||
		got.Status != "active" || got.ThumbnailSource != "http://img/1.jpg" || got.SyncedAt == nil || !got.SyncedAt.Equal(now) {
		t.Fatalf("listing = %+v", got)
	}

	cases := []struct {
		name string
		item *RemoteItem
		want error
	}{
		{"another seller", &RemoteItem{ID: "MLB123", SellerID: "999"}, ErrListingNotOwned},
		{"no seller", &RemoteItem{ID: "MLB123"}, ErrListingNotOwned},
		{"bad item id", &RemoteItem{ID: "MLA123", SellerID: "415458330"}, ErrInvalidResource},
		{"nil item", nil, ErrInvalidResource},
	}
	for _, tc := range cases {
		if _, err := ListingFromRemote(sellerAccount(), tc.item, now); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestAThumbnailIsRehostedOnlyWhenItsSourceChanged(t *testing.T) {
	cases := []struct {
		name    string
		stored  *Listing
		source  string
		rehosts bool
	}{
		{"never stored", nil, "http://img/1.jpg", true},
		{"stored without a file", &Listing{ThumbnailSource: "http://img/1.jpg"}, "http://img/1.jpg", true},
		{"same source", &Listing{ThumbnailKey: "k", ThumbnailSource: "http://img/1.jpg"}, "http://img/1.jpg", false},
		{"new source", &Listing{ThumbnailKey: "k", ThumbnailSource: "http://img/1.jpg"}, "http://img/2.jpg", true},
		{"no source", nil, "  ", false},
	}
	for _, tc := range cases {
		if got := tc.stored.ThumbnailChanged(tc.source); got != tc.rehosts {
			t.Errorf("%s: ThumbnailChanged = %v", tc.name, got)
		}
	}
}

func TestTheThumbnailKeyIsScopedToTheAccountAndItem(t *testing.T) {
	if got := ListingThumbnailKeyBase("acc-1", "MLB123"); got != "mercadolivre/accounts/acc-1/listings/MLB123/thumbnail" {
		t.Fatalf("key = %q", got)
	}
}

func TestListingsRefreshEverySixHours(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	fresh, stale := now.Add(-5*time.Hour), now.Add(-6*time.Hour)
	if (&Listing{SyncedAt: &fresh}).NeedsRefresh(now) {
		t.Fatal("a listing synced 5 hours ago is fresh")
	}
	if !(&Listing{SyncedAt: &stale}).NeedsRefresh(now) || !(&Listing{}).NeedsRefresh(now) {
		t.Fatal("a listing synced 6 hours ago, or never, needs a refresh")
	}
}

func TestListLimitsAreBounded(t *testing.T) {
	cases := map[int]int{0: DefaultListLimit, -3: DefaultListLimit, 10: 10, MaxListLimit: MaxListLimit, MaxListLimit + 1: MaxListLimit}
	for in, want := range cases {
		if got := NormalizeListLimit(in); got != want {
			t.Errorf("NormalizeListLimit(%d) = %d, want %d", in, got, want)
		}
	}
}
