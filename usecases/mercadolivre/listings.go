package mercadolivre

import (
	"context"
	"errors"
	"fmt"
	"log"

	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	"vozko/pkg/bounded"
)

const (
	listingMaxPages        = 500
	listingBulkConcurrency = 4
	thumbnailConcurrency   = 4
)

type ThumbnailStore interface {
	Rehost(ctx context.Context, keyBase, url string) (string, error)
	URL(key string) string
}

type ThumbnailFetcher struct {
	media mldomain.MediaFetcher
}

func NewThumbnailFetcher(media mldomain.MediaFetcher) ThumbnailFetcher {
	if media == nil {
		panic("mercadolivre: the thumbnail fetcher requires the media fetcher")
	}
	return ThumbnailFetcher{media: media}
}

func (f ThumbnailFetcher) FetchBytes(ctx context.Context, url string) ([]byte, string, error) {
	file, err := f.media.Fetch(ctx, url, mldomain.MaxThumbnailBytes)
	if err != nil {
		return nil, "", err
	}
	return file.Data, file.ContentType, nil
}

type ListingsDeps struct {
	Accounts mldomain.AccountRepository
	Listings mldomain.ListingRepository
	Services Services
	Tokens   *TokenSource
	Images   ThumbnailStore
	Metrics  Metrics
}

type Listings struct {
	accounts mldomain.AccountRepository
	listings mldomain.ListingRepository
	svc      Services
	tokens   *TokenSource
	images   ThumbnailStore
	metrics  Metrics
	now      Clock
}

var _ AccountReconciler = (*Listings)(nil)

func NewListings(d ListingsDeps) *Listings {
	if d.Accounts == nil || d.Listings == nil || d.Tokens == nil || d.Images == nil || d.Metrics == nil ||
		d.Services.Items == nil || d.Services.Errors == nil {
		panic("mercadolivre: listings require accounts, the listing mirror, items, an error classifier, tokens, thumbnail storage and metrics")
	}
	return &Listings{accounts: d.Accounts, listings: d.Listings, svc: d.Services, tokens: d.Tokens, images: d.Images, metrics: d.Metrics, now: utcNow}
}

func (l *Listings) Reconcile(ctx context.Context, account *mldomain.Account, creds mldomain.Credentials, _ string) (int, error) {
	now := l.now()
	claimed, err := l.listings.ClaimSync(ctx, account.ID, now, now.Add(-mldomain.ListingFreshness))
	if err != nil {
		return 0, fmt.Errorf("mercadolivre: listing sync of %s not claimed: %w", account.ID, err)
	}
	if !claimed {
		return 0, nil
	}
	synced, err := l.sync(ctx, account, creds)
	if err != nil {
		if releaseErr := l.listings.ReleaseSync(ctx, account.ID); releaseErr != nil {
			log.Printf("[mercadolivre][listings] sync claim of %s not released: %v", account.ID, releaseErr)
		}
		l.metrics.AddMercadoLivreEvent("listings", "sync_failed")
		return synced, err
	}
	l.metrics.AddMercadoLivreEvent("listings", "synced")
	return synced, nil
}

func (l *Listings) sync(ctx context.Context, account *mldomain.Account, creds mldomain.Credentials) (int, error) {
	synced, scroll := 0, ""
	for page := 0; page < listingMaxPages; page++ {
		found, err := l.svc.Items.SearchSellerItems(ctx, creds, mldomain.ItemSearch{Scan: true, ScrollID: scroll, Limit: mldomain.MaxItemSearchLimit})
		if err != nil {
			return synced, l.apiErr(ctx, account, err)
		}
		if len(found.IDs) == 0 {
			return synced, nil
		}
		n, err := l.ingest(ctx, account, creds, found.IDs)
		synced += n
		if err != nil {
			return synced, err
		}
		if found.ScrollID == "" {
			return synced, nil
		}
		scroll = found.ScrollID
	}
	return synced, nil
}

func (l *Listings) apiErr(ctx context.Context, account *mldomain.Account, err error) error {
	return classifyAPIError(ctx, l.svc.Errors, l.tokens, l.metrics, account, err)
}

func (l *Listings) ingest(ctx context.Context, account *mldomain.Account, creds mldomain.Credentials, ids []string) (int, error) {
	batches := chunk(ids, mldomain.MaxItemsPerBatch)
	results := make([][]*mldomain.RemoteItem, len(batches))
	errs := make([]error, len(batches))
	indexes := make([]int, len(batches))
	for i := range indexes {
		indexes[i] = i
	}
	bounded.ForEach(ctx, indexes, listingBulkConcurrency, func(ctx context.Context, i int) {
		items, _, err := l.svc.Items.GetItems(ctx, creds, batches[i])
		results[i], errs[i] = items, err
	})
	if err := errors.Join(errs...); err != nil {
		return 0, l.apiErr(ctx, account, err)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var remote []*mldomain.RemoteItem
	for _, r := range results {
		remote = append(remote, r...)
	}
	return l.store(ctx, account, remote)
}

func (l *Listings) store(ctx context.Context, account *mldomain.Account, remote []*mldomain.RemoteItem) (int, error) {
	now := l.now()
	listings := make([]*mldomain.Listing, 0, len(remote))
	itemIDs := make([]string, 0, len(remote))
	for _, item := range remote {
		listing, err := mldomain.ListingFromRemote(account, item, now)
		if err != nil {
			l.metrics.AddMercadoLivreEvent("listings", "not_owned")
			continue
		}
		listings = append(listings, listing)
		itemIDs = append(itemIDs, listing.ItemID)
	}
	if len(listings) == 0 {
		return 0, nil
	}
	existing, err := l.listings.FindByItemIDs(ctx, account.ID, itemIDs)
	if err != nil {
		return 0, err
	}
	known := make(map[string]*mldomain.Listing, len(existing))
	for _, e := range existing {
		known[e.ItemID] = e
	}
	if err := l.listings.UpsertMany(ctx, listings); err != nil {
		return 0, err
	}
	var changed []*mldomain.Listing
	for _, listing := range listings {
		if known[listing.ItemID].ThumbnailChanged(listing.ThumbnailSource) {
			changed = append(changed, listing)
		}
	}
	bounded.ForEach(ctx, changed, thumbnailConcurrency, func(ctx context.Context, listing *mldomain.Listing) {
		l.rehost(ctx, account, listing)
	})
	return len(listings), nil
}

func (l *Listings) rehost(ctx context.Context, account *mldomain.Account, listing *mldomain.Listing) {
	key, err := l.images.Rehost(ctx, mldomain.ListingThumbnailKeyBase(account.ID, listing.ItemID), listing.ThumbnailSource)
	if err == nil {
		err = l.listings.SetThumbnail(ctx, account.ID, listing.ItemID, key, listing.ThumbnailSource)
	}
	if err != nil {
		l.metrics.AddMercadoLivreEvent("listings", "thumbnail_failed")
		log.Printf("[mercadolivre][listings] thumbnail of %s kept as it was: %v", listing.ItemID, err)
	}
}

func (l *Listings) Handle(ctx context.Context, account *mldomain.Account, job mldomain.Job) error {
	if job.Kind != mldomain.JobFetchItem {
		return fmt.Errorf("%w: %s", ErrJobKindNotHandled, job.Kind)
	}
	if !mldomain.ValidItemID(job.ResourceID) {
		return fmt.Errorf("%w: item %q", mldomain.ErrInvalidResource, job.ResourceID)
	}
	creds, err := l.tokens.Credentials(ctx, account)
	if errors.Is(err, mldomain.ErrNeedsReconnect) {
		return fmt.Errorf("%w: %v", ErrAccountUnavailable, err)
	}
	if err != nil {
		return err
	}
	items, _, err := l.svc.Items.GetItems(ctx, creds, []string{job.ResourceID})
	if err != nil {
		return l.apiErr(ctx, account, err)
	}
	if len(items) == 0 {
		return fmt.Errorf("%w: item %s", ErrResourceGone, job.ResourceID)
	}
	_, err = l.store(ctx, account, items)
	return err
}

type ListingsQuery struct {
	WorkspaceID string
	AccountID   string
	Search      string
	Status      string
	Cursor      string
	Limit       int
}

type ListingView struct {
	*mldomain.Listing
	ThumbnailURL string
}

type ListingPage struct {
	Items      []ListingView
	NextCursor string
}

func (l *Listings) List(ctx context.Context, q ListingsQuery) (*ListingPage, error) {
	account, err := l.accounts.FindByIDInWorkspace(ctx, q.WorkspaceID, q.AccountID)
	if err != nil {
		return nil, err
	}
	after, _, err := shared.ParseKeyset(q.Cursor)
	if err != nil {
		return nil, err
	}
	in := mldomain.ListListingsInput{AccountID: account.ID, Search: q.Search, Status: q.Status, Limit: q.Limit}
	if q.Cursor != "" {
		in.After = &after
	}
	list, err := l.listings.ListByAccount(ctx, in)
	if err != nil {
		return nil, err
	}
	page := &ListingPage{Items: make([]ListingView, 0, len(list.Items))}
	for _, listing := range list.Items {
		page.Items = append(page.Items, l.view(listing))
	}
	if list.Next != nil {
		page.NextCursor = list.Next.Encode()
	}
	return page, nil
}

func (l *Listings) view(listing *mldomain.Listing) ListingView {
	return ListingView{Listing: listing, ThumbnailURL: l.images.URL(listing.ThumbnailKey)}
}

func (l *Listings) views(ctx context.Context, accountID string, itemIDs []string) (map[string]ListingView, error) {
	found, err := l.listings.FindByItemIDs(ctx, accountID, itemIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ListingView, len(found))
	for _, listing := range found {
		out[listing.ItemID] = l.view(listing)
	}
	return out, nil
}

func (l *Listings) ThumbnailURL(ctx context.Context, workspaceID, accountID, itemID string) (string, error) {
	account, err := l.accounts.FindByIDInWorkspace(ctx, workspaceID, accountID)
	if err != nil {
		return "", err
	}
	listing, err := l.listings.FindByItemID(ctx, account.ID, itemID)
	if err != nil {
		return "", err
	}
	url := l.images.URL(listing.ThumbnailKey)
	if url == "" {
		return "", mldomain.ErrListingNotFound
	}
	return url, nil
}

func chunk(ids []string, size int) [][]string {
	var out [][]string
	for start := 0; start < len(ids); start += size {
		out = append(out, ids[start:min(start+size, len(ids))])
	}
	return out
}
