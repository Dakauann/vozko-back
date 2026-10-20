package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"

	mldomain "vozko/domain/mercadolivre"
)

type MissedFeedService struct {
	c *Client
}

var _ mldomain.MissedFeedService = (*MissedFeedService)(nil)

func NewMissedFeedService(c *Client) (*MissedFeedService, error) {
	if c == nil {
		return nil, errors.New("mercadolivre: missed feed service requires a client")
	}
	return &MissedFeedService{c: c}, nil
}

func (s *MissedFeedService) MissedFeeds(ctx context.Context, creds mldomain.Credentials, query mldomain.MissedFeedQuery) ([]mldomain.Notification, error) {
	if !mldomain.ValidNumericID(query.AppID) {
		return nil, errors.New("mercadolivre: missed feeds need the numeric app id")
	}
	q := url.Values{}
	q.Set("app_id", query.AppID)
	if query.Topic != "" {
		if !slices.Contains(mldomain.SubscribedTopics(), query.Topic) {
			return nil, fmt.Errorf("%w: %q", mldomain.ErrUnknownTopic, query.Topic)
		}
		q.Set("topic", string(query.Topic))
	}
	if query.Topic == mldomain.TopicItems {
		if !mldomain.SupportedSite(query.SiteID) {
			return nil, fmt.Errorf("%w: missed items need a site, got %q", mldomain.ErrSiteNotSupported, query.SiteID)
		}
		q.Set("site_id", query.SiteID)
	}
	q.Set("offset", strconv.Itoa(max(query.Offset, 0)))
	q.Set("limit", strconv.Itoa(boundLimit(query.Limit, mldomain.MaxMissedFeedLimit)))
	var w struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := s.c.getJSON(ctx, request{path: "/missed_feeds", query: q, family: FamilyOther, creds: creds}, &w); err != nil {
		return nil, err
	}
	out := make([]mldomain.Notification, 0, len(w.Messages))
	for _, raw := range w.Messages {
		n, err := mldomain.Decode(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}
