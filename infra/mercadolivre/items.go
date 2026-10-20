package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	mldomain "vozko/domain/mercadolivre"
)

type ItemService struct {
	c *Client
}

var _ mldomain.ItemService = (*ItemService)(nil)

func NewItemService(c *Client) (*ItemService, error) {
	if c == nil {
		return nil, errors.New("mercadolivre: item service requires a client")
	}
	return &ItemService{c: c}, nil
}

type itemSearchWire struct {
	Paging   *paging  `json:"paging"`
	Results  []string `json:"results"`
	ScrollID string   `json:"scroll_id"`
}

func (s *ItemService) SearchSellerItems(ctx context.Context, creds mldomain.Credentials, search mldomain.ItemSearch) (*mldomain.ItemIDPage, error) {
	q := url.Values{}
	if search.Status != "" {
		q.Set("status", search.Status)
	}
	q.Set("limit", strconv.Itoa(boundLimit(search.Limit, mldomain.MaxItemSearchLimit)))
	if search.Scan {
		q.Set("search_type", "scan")
		if search.ScrollID != "" {
			q.Set("scroll_id", search.ScrollID)
		}
	} else {
		q.Set("offset", strconv.Itoa(max(search.Offset, 0)))
	}
	var w itemSearchWire
	if err := s.c.getJSON(ctx, request{
		path:   "/users/" + pathEscape(creds.UserID) + "/items/search",
		query:  q,
		family: FamilyItems,
		creds:  creds,
	}, &w); err != nil {
		return nil, err
	}
	page := &mldomain.ItemIDPage{IDs: w.Results, ScrollID: w.ScrollID}
	if w.Paging != nil {
		page.Total, page.Offset, page.Limit = w.Paging.Total, w.Paging.Offset, w.Paging.Limit
	}
	return page, nil
}

type itemWire struct {
	ID        string              `json:"id"`
	SiteID    string              `json:"site_id"`
	SellerID  mldomain.FlexibleID `json:"seller_id"`
	Title     string              `json:"title"`
	Permalink string              `json:"permalink"`
	Thumbnail string              `json:"thumbnail"`
	Status    string              `json:"status"`
}

type bulkItemWire struct {
	ID         string          `json:"id"`
	StatusCode int             `json:"status_code"`
	Code       int             `json:"code"`
	Body       json.RawMessage `json:"body"`
}

func (s *ItemService) GetItems(ctx context.Context, creds mldomain.Credentials, ids []string) ([]*mldomain.RemoteItem, []mldomain.ItemFailure, error) {
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !mldomain.ValidItemID(id) {
			return nil, nil, fmt.Errorf("%w: item %q", mldomain.ErrInvalidResource, id)
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 || len(unique) > mldomain.MaxItemsPerBatch {
		return nil, nil, fmt.Errorf("mercadolivre: a bulk read takes 1 to %d items, got %d", mldomain.MaxItemsPerBatch, len(unique))
	}
	q := url.Values{}
	q.Set("ids", strings.Join(unique, ","))
	var entries []bulkItemWire
	if err := s.c.getJSON(ctx, request{
		path:   "/items/bulk",
		query:  q,
		family: FamilyItems,
		creds:  creds,
	}, &entries); err != nil {
		return nil, nil, err
	}
	var items []*mldomain.RemoteItem
	var failures []mldomain.ItemFailure
	for _, e := range entries {
		status := e.StatusCode
		if status == 0 {
			status = e.Code
		}
		var body itemWire
		decodeErr := json.Unmarshal(e.Body, &body)
		id := e.ID
		if id == "" {
			id = body.ID
		}
		if status != 200 || decodeErr != nil || !mldomain.ValidItemID(body.ID) {
			failures = append(failures, mldomain.ItemFailure{ID: id, StatusCode: status})
			continue
		}
		items = append(items, &mldomain.RemoteItem{
			ID:        body.ID,
			SiteID:    body.SiteID,
			SellerID:  body.SellerID.String(),
			Title:     body.Title,
			Permalink: body.Permalink,
			Thumbnail: body.Thumbnail,
			Status:    body.Status,
		})
	}
	return items, failures, nil
}
