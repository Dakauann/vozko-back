package tiktok

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	ttdomain "vozko/domain/tiktok"
)

var videoFields = []string{"item_id", "media_type", "caption", "create_time", "share_url", "embed_url", "thumbnail_url", "is_ad", "likes", "comments", "shares", "video_views"}

type wireVideo struct {
	ItemID       ttdomain.NumericID `json:"item_id"`
	MediaType    string             `json:"media_type"`
	Caption      string             `json:"caption"`
	CreateTime   json.Number        `json:"create_time"`
	ShareURL     string             `json:"share_url"`
	EmbedURL     string             `json:"embed_url"`
	ThumbnailURL string             `json:"thumbnail_url"`
	IsAd         bool               `json:"is_ad"`
	Likes        int64              `json:"likes"`
	Comments     int64              `json:"comments"`
	Shares       int64              `json:"shares"`
	VideoViews   int64              `json:"video_views"`
}

func (v wireVideo) remote() ttdomain.RemoteVideo {
	media := ttdomain.MediaTypeVideo
	if strings.EqualFold(v.MediaType, string(ttdomain.MediaTypePhoto)) {
		media = ttdomain.MediaTypePhoto
	}
	return ttdomain.RemoteVideo{
		ItemID:       v.ItemID.String(),
		MediaType:    media,
		Caption:      v.Caption,
		ShareURL:     strings.TrimSpace(v.ShareURL),
		EmbedURL:     strings.TrimSpace(v.EmbedURL),
		ThumbnailURL: strings.TrimSpace(v.ThumbnailURL),
		IsAd:         v.IsAd,
		Likes:        v.Likes,
		Comments:     v.Comments,
		Shares:       v.Shares,
		Views:        v.VideoViews,
		PostedAt:     unixSeconds(v.CreateTime),
	}
}

func unixSeconds(n json.Number) *time.Time {
	secs, err := strconv.ParseInt(strings.TrimSpace(n.String()), 10, 64)
	if err != nil || secs <= 0 {
		return nil
	}
	t := time.Unix(secs, 0).UTC()
	return &t
}

type videoListResponse struct {
	Videos  []wireVideo `json:"videos"`
	Cursor  int64       `json:"cursor"`
	HasMore bool        `json:"has_more"`
}

func (c *Client) ListVideos(ctx context.Context, creds ttdomain.Credentials, cursor int64, limit int) (ttdomain.VideoPage, error) {
	if limit <= 0 || limit > ttdomain.VideoListPage {
		limit = ttdomain.VideoListPage
	}
	q := url.Values{}
	q.Set("business_id", creds.OpenID)
	q.Set("fields", jsonList(videoFields))
	q.Set("max_count", strconv.Itoa(limit))
	if cursor > 0 {
		q.Set("cursor", strconv.FormatInt(cursor, 10))
	}
	var out videoListResponse
	if err := c.getJSON(ctx, request{path: "/business/video/list/", query: q, family: FamilyVideos, creds: &creds}, &out); err != nil {
		return ttdomain.VideoPage{}, err
	}
	page := ttdomain.VideoPage{Cursor: out.Cursor, HasMore: out.HasMore}
	for _, v := range out.Videos {
		if v.ItemID == "" {
			continue
		}
		page.Videos = append(page.Videos, v.remote())
	}
	return page, nil
}

func (c *Client) GetVideos(ctx context.Context, creds ttdomain.Credentials, itemIDs []string) ([]ttdomain.RemoteVideo, error) {
	ids := make([]string, 0, len(itemIDs))
	for _, id := range itemIDs {
		if ttdomain.ValidNumericID(id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	filters, err := json.Marshal(map[string][]string{"video_ids": ids})
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("business_id", creds.OpenID)
	q.Set("fields", jsonList(videoFields))
	q.Set("filters", string(filters))
	q.Set("max_count", strconv.Itoa(min(len(ids), ttdomain.VideoListPage)))
	var out videoListResponse
	if err := c.getJSON(ctx, request{path: "/business/video/list/", query: q, family: FamilyVideos, creds: &creds}, &out); err != nil {
		return nil, err
	}
	videos := make([]ttdomain.RemoteVideo, 0, len(out.Videos))
	for _, v := range out.Videos {
		if v.ItemID != "" {
			videos = append(videos, v.remote())
		}
	}
	return videos, nil
}
