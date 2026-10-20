package mercadolivre

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	mldomain "vozko/domain/mercadolivre"
	"vozko/pkg/bounded"
)

const maxOrdersPerPack = 10

type PackSummariesDeps struct {
	Conversations mldomain.ConversationRepository
	Listings      mldomain.ListingRepository
	Services      Services
	Images        ThumbnailStore
	Metrics       Metrics
}

type PackSummaries struct {
	conversations mldomain.ConversationRepository
	listings      mldomain.ListingRepository
	svc           Services
	images        ThumbnailStore
	metrics       Metrics
	now           Clock
}

func NewPackSummaries(d PackSummariesDeps) *PackSummaries {
	if d.Conversations == nil || d.Listings == nil || d.Images == nil || d.Metrics == nil ||
		d.Services.Packs == nil || d.Services.Orders == nil || d.Services.Shipments == nil || d.Services.Items == nil || d.Services.Errors == nil {
		panic("mercadolivre: pack summaries require conversations, the listing mirror, packs, orders, shipments, items, an error classifier, thumbnail storage and metrics")
	}
	return &PackSummaries{
		conversations: d.Conversations, listings: d.Listings, svc: d.Services, images: d.Images, metrics: d.Metrics, now: utcNow,
	}
}

func (p *PackSummaries) Refresh(ctx context.Context, creds mldomain.Credentials, conv *mldomain.Conversation) error {
	summary := mldomain.PackSummary{OrderIDs: []string{}, Items: []mldomain.PackItem{}}
	orderIDs := []string{conv.PackKey}
	pack, err := p.svc.Packs.GetPack(ctx, creds, conv.PackKey)
	switch {
	case err == nil:
		summary.PackID = pack.ID
		summary.ShipmentID = pack.ShipmentID
		if len(pack.OrderIDs) > 0 {
			orderIDs = pack.OrderIDs
		}
	case p.svc.Errors.IsNotFound(err):
	default:
		return fmt.Errorf("mercadolivre: read pack %s: %w", conv.PackKey, err)
	}
	if len(orderIDs) > maxOrdersPerPack {
		orderIDs = orderIDs[:maxOrdersPerPack]
	}
	for _, id := range orderIDs {
		order, err := p.svc.Orders.GetOrder(ctx, creds, id)
		if err != nil {
			return fmt.Errorf("mercadolivre: read order %s: %w", id, err)
		}
		summary.OrderIDs = append(summary.OrderIDs, order.ID)
		if summary.OrderStatus == "" {
			summary.OrderStatus = order.Status
		}
		if summary.ShipmentID == "" {
			summary.ShipmentID = order.ShipmentID
		}
		if summary.CurrencyID == "" {
			summary.CurrencyID = order.CurrencyID
		}
		summary.TotalAmount += order.TotalAmount
		for _, item := range order.Items {
			summary.Items = append(summary.Items, mldomain.PackItem{
				ItemID: item.ItemID, Title: item.Title, Quantity: item.Quantity, UnitPrice: item.UnitPrice, CurrencyID: item.CurrencyID,
			})
		}
	}
	if mldomain.ValidNumericID(summary.ShipmentID) {
		shipment, err := p.svc.Shipments.GetShipment(ctx, creds, summary.ShipmentID)
		switch {
		case err == nil:
			summary.ShippingStatus, summary.ShippingSubstatus = shipment.Status, shipment.Substatus
		case p.svc.Errors.IsNotFound(err):
		default:
			return fmt.Errorf("mercadolivre: read shipment %s: %w", summary.ShipmentID, err)
		}
	}
	if err := p.attachThumbnails(ctx, creds, conv, &summary); err != nil {
		return err
	}
	return p.conversations.UpdatePackSummary(ctx, conv.ID, summary, p.now())
}

type packThumbnail struct {
	key       string
	permalink string
}

func (p *PackSummaries) attachThumbnails(ctx context.Context, creds mldomain.Credentials, conv *mldomain.Conversation, summary *mldomain.PackSummary) error {
	missing, err := p.itemsOutsideTheMirror(ctx, conv, summary)
	if err != nil {
		return err
	}
	stored := p.storedThumbnails(conv)
	var download []string
	for _, id := range missing {
		if _, ok := stored[id]; !ok {
			download = append(download, id)
		}
	}
	for id, thumb := range p.rehost(ctx, creds, conv, download) {
		stored[id] = thumb
	}
	for i := range summary.Items {
		if thumb, ok := stored[summary.Items[i].ItemID]; ok {
			summary.Items[i].ThumbnailKey, summary.Items[i].Permalink = thumb.key, thumb.permalink
		}
	}
	return nil
}

func (p *PackSummaries) itemsOutsideTheMirror(ctx context.Context, conv *mldomain.Conversation, summary *mldomain.PackSummary) ([]string, error) {
	var ids []string
	seen := map[string]bool{}
	for _, item := range summary.Items {
		if mldomain.ValidItemID(item.ItemID) && !seen[item.ItemID] {
			seen[item.ItemID] = true
			ids = append(ids, item.ItemID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	mirrored, err := p.listings.FindByItemIDs(ctx, conv.AccountID, ids)
	if err != nil {
		return nil, fmt.Errorf("mercadolivre: read the listing mirror for pack %s: %w", conv.PackKey, err)
	}
	inMirror := make(map[string]bool, len(mirrored))
	for _, listing := range mirrored {
		if listing.ThumbnailKey != "" {
			inMirror[listing.ItemID] = true
		}
	}
	var missing []string
	for _, id := range ids {
		if !inMirror[id] {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

func (p *PackSummaries) storedThumbnails(conv *mldomain.Conversation) map[string]packThumbnail {
	out := map[string]packThumbnail{}
	if conv.PackSummary == nil {
		return out
	}
	for _, item := range conv.PackSummary.Items {
		if item.ThumbnailKey != "" {
			out[item.ItemID] = packThumbnail{key: item.ThumbnailKey, permalink: item.Permalink}
		}
	}
	return out
}

func (p *PackSummaries) rehost(ctx context.Context, creds mldomain.Credentials, conv *mldomain.Conversation, itemIDs []string) map[string]packThumbnail {
	out := map[string]packThumbnail{}
	var remote []*mldomain.RemoteItem
	for _, batch := range chunk(itemIDs, mldomain.MaxItemsPerBatch) {
		items, _, err := p.svc.Items.GetItems(ctx, creds, batch)
		if err != nil {
			p.metrics.AddMercadoLivreEvent("pack_summary", "thumbnail_failed")
			log.Printf("[mercadolivre][pack] items of pack %s not read, thumbnails left out: %v", conv.PackKey, err)
			continue
		}
		remote = append(remote, items...)
	}
	asked := make(map[string]bool, len(itemIDs))
	for _, id := range itemIDs {
		asked[id] = true
	}
	var mu sync.Mutex
	bounded.ForEach(ctx, remote, thumbnailConcurrency, func(ctx context.Context, item *mldomain.RemoteItem) {
		if item == nil || !asked[item.ID] {
			return
		}
		source := strings.TrimSpace(item.Thumbnail)
		if source == "" {
			return
		}
		key, err := p.images.Rehost(ctx, mldomain.PackItemThumbnailKeyBase(conv.AccountID, conv.ID, item.ID), source)
		if err != nil {
			p.metrics.AddMercadoLivreEvent("pack_summary", "thumbnail_failed")
			log.Printf("[mercadolivre][pack] thumbnail of %s in pack %s left out: %v", item.ID, conv.PackKey, err)
			return
		}
		mu.Lock()
		out[item.ID] = packThumbnail{key: key, permalink: strings.TrimSpace(item.Permalink)}
		mu.Unlock()
	})
	return out
}
