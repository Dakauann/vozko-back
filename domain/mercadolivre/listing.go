package mercadolivre

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/shared"
)

const ListingFreshness = 6 * time.Hour

const MaxThumbnailBytes = 5 << 20

const (
	DefaultListLimit = 50
	MaxListLimit     = 100
)

func NormalizeListLimit(limit int) int {
	if limit <= 0 {
		return DefaultListLimit
	}
	return min(limit, MaxListLimit)
}

type Listing struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	AccountID   string `json:"accountId"`

	ItemID          string `json:"itemId"`
	Title           string `json:"title"`
	Permalink       string `json:"permalink,omitempty"`
	ThumbnailKey    string `json:"thumbnailKey,omitempty"`
	ThumbnailSource string `json:"-"`
	Status          string `json:"status"`

	SyncedAt *time.Time `json:"syncedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (l *Listing) NeedsRefresh(now time.Time) bool {
	if l.SyncedAt == nil {
		return true
	}
	return now.Sub(*l.SyncedAt) >= ListingFreshness
}

func (l *Listing) ThumbnailChanged(source string) bool {
	source = strings.TrimSpace(source)
	if source == "" {
		return false
	}
	return l == nil || l.ThumbnailKey == "" || l.ThumbnailSource != source
}

func ListingThumbnailKeyBase(accountID, itemID string) string {
	return "mercadolivre/accounts/" + accountID + "/listings/" + itemID + "/thumbnail"
}

func ListingFromRemote(account *Account, item *RemoteItem, now time.Time) (*Listing, error) {
	if account == nil || item == nil || !ValidItemID(item.ID) {
		return nil, fmt.Errorf("%w: listing", ErrInvalidResource)
	}
	if item.SellerID == "" || item.SellerID != account.MLUserID {
		return nil, fmt.Errorf("%w: %s", ErrListingNotOwned, item.ID)
	}
	synced := now
	return &Listing{
		WorkspaceID:     account.WorkspaceID,
		AccountID:       account.ID,
		ItemID:          item.ID,
		Title:           strings.TrimSpace(item.Title),
		Permalink:       strings.TrimSpace(item.Permalink),
		ThumbnailSource: strings.TrimSpace(item.Thumbnail),
		Status:          strings.TrimSpace(item.Status),
		SyncedAt:        &synced,
	}, nil
}

type ListingThumbnail struct {
	ItemID string
	Key    string
}

type ListingList struct {
	Items []*Listing
	Next  *shared.Keyset
}
