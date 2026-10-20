package mercadolivre

import (
	"context"
	"errors"
	"log"
	"time"

	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
)

type PacksDeps struct {
	Accounts      mldomain.AccountRepository
	Conversations mldomain.ConversationRepository
	Listings      mldomain.ListingRepository
	Images        ThumbnailStore
	Refresh       *JobPublisher
	Access        shared.EntryAccessChecker
	Metrics       Metrics
}

type Packs struct {
	accounts mldomain.AccountRepository
	listings mldomain.ListingRepository
	images   ThumbnailStore
	refresh  *JobPublisher
	guard    conversationGuard
	metrics  Metrics
	now      Clock
}

func NewPacks(d PacksDeps) *Packs {
	if d.Accounts == nil || d.Conversations == nil || d.Listings == nil || d.Images == nil || d.Refresh == nil || d.Access == nil || d.Metrics == nil {
		panic("mercadolivre: pack summaries need accounts, conversations, the listing mirror, thumbnail storage, the job publisher, entry access and metrics")
	}
	return &Packs{
		accounts: d.Accounts, listings: d.Listings, images: d.Images, refresh: d.Refresh,
		guard:   conversationGuard{access: d.Access, conversations: d.Conversations},
		metrics: d.Metrics, now: utcNow,
	}
}

type PackItemView struct {
	ItemID       string
	Title        string
	Quantity     int
	UnitPrice    float64
	CurrencyID   string
	ThumbnailURL string
	Permalink    string
}

type PackView struct {
	Items             []PackItemView
	OrderStatus       string
	ShippingStatus    string
	ShippingSubstatus string
	TotalAmount       float64
	CurrencyID        string
	RefreshedAt       time.Time
}

func (p *Packs) Summary(ctx context.Context, q ConversationQuery) (*PackView, error) {
	_, conv, err := p.guard.authorize(ctx, q)
	if err != nil {
		return nil, err
	}
	if conv.PackSummaryIsStale(p.now()) {
		p.queueRefresh(ctx, conv)
	}
	if conv.PackSummary == nil || conv.PackSummaryRefreshedAt == nil {
		return nil, mldomain.ErrPackSummaryPending
	}
	return p.view(ctx, conv)
}

func (p *Packs) queueRefresh(ctx context.Context, conv *mldomain.Conversation) {
	account, err := p.accounts.FindByID(ctx, conv.AccountID)
	if errors.Is(err, mldomain.ErrAccountNotFound) {
		return
	}
	if err == nil && !account.CanReceiveMessages() {
		return
	}
	if err == nil {
		err = p.publishRefresh(account, conv)
	}
	if err != nil {
		p.metrics.AddMercadoLivreEvent("pack_summary", "refresh_not_queued")
		log.Printf("[mercadolivre][pack] stale summary of %s kept, refresh not queued: %v", conv.ID, err)
	}
}

func (p *Packs) publishRefresh(account *mldomain.Account, conv *mldomain.Conversation) error {
	marker := "stale:" + p.now().Truncate(staleRereadBucket).Format("200601021504")
	job, err := mldomain.PackJob(account.MLUserID, conv.PackKey, marker)
	if err != nil {
		return err
	}
	return p.refresh.Publish(job)
}

func (p *Packs) view(ctx context.Context, conv *mldomain.Conversation) (*PackView, error) {
	summary := conv.PackSummary
	itemIDs := make([]string, 0, len(summary.Items))
	for _, item := range summary.Items {
		itemIDs = append(itemIDs, item.ItemID)
	}
	mirrored, err := p.listings.FindByItemIDs(ctx, conv.AccountID, shared.UniqueNonEmpty(itemIDs))
	if err != nil {
		return nil, err
	}
	listings := make(map[string]*mldomain.Listing, len(mirrored))
	for _, listing := range mirrored {
		listings[listing.ItemID] = listing
	}
	view := &PackView{
		Items:       make([]PackItemView, 0, len(summary.Items)),
		OrderStatus: summary.OrderStatus, ShippingStatus: summary.ShippingStatus, ShippingSubstatus: summary.ShippingSubstatus,
		TotalAmount: summary.TotalAmount, CurrencyID: summary.CurrencyID, RefreshedAt: *conv.PackSummaryRefreshedAt,
	}
	for _, item := range summary.Items {
		view.Items = append(view.Items, p.itemView(item, listings[item.ItemID], summary.CurrencyID))
	}
	return view, nil
}

func (p *Packs) itemView(item mldomain.PackItem, listing *mldomain.Listing, packCurrency string) PackItemView {
	out := PackItemView{
		ItemID: item.ItemID, Title: item.Title, Quantity: item.Quantity, UnitPrice: item.UnitPrice,
		CurrencyID: item.CurrencyID, ThumbnailURL: p.images.URL(item.ThumbnailKey), Permalink: item.Permalink,
	}
	if out.CurrencyID == "" {
		out.CurrencyID = packCurrency
	}
	if listing != nil && listing.ThumbnailKey != "" {
		out.ThumbnailURL = p.images.URL(listing.ThumbnailKey)
	}
	if listing != nil && listing.Permalink != "" {
		out.Permalink = listing.Permalink
	}
	return out
}
