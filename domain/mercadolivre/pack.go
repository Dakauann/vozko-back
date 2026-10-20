package mercadolivre

import (
	"errors"
	"fmt"
	"strings"
)

var ErrPackSummaryPending = errors.New("mercado livre pack summary is not loaded yet")

func PackJob(sellerID, packKey, marker string) (Job, error) {
	if !ValidNumericID(sellerID) {
		return Job{}, ErrUserIDInvalid
	}
	if !ValidNumericID(packKey) {
		return Job{}, fmt.Errorf("%w: pack %q", ErrInvalidResource, packKey)
	}
	if strings.TrimSpace(marker) == "" {
		return Job{}, ErrNotificationIdentity
	}
	return Job{
		Kind:       JobRefreshPack,
		Topic:      "pack",
		SellerID:   sellerID,
		ResourceID: packKey,
		Resource:   "/packs/" + packKey,
		Marker:     marker,
	}, nil
}

func PackItemThumbnailKeyBase(accountID, conversationID, itemID string) string {
	return "mercadolivre/accounts/" + accountID + "/conversations/" + conversationID + "/items/" + itemID + "/thumbnail"
}

func (s *PackSummary) ThumbnailKeys() []string {
	if s == nil {
		return nil
	}
	var keys []string
	seen := map[string]bool{}
	for _, item := range s.Items {
		if item.ThumbnailKey == "" || seen[item.ThumbnailKey] {
			continue
		}
		seen[item.ThumbnailKey] = true
		keys = append(keys, item.ThumbnailKey)
	}
	return keys
}
