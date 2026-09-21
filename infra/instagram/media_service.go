package instagram

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	igdomain "vozko/domain/instagram"
	"vozko/infra/meta"
)

type mediaService struct {
	client *meta.Client
}

const DefaultGraphVersion = "v25.0"

type GraphConfig struct {
	GraphVersion string
	AppSecret    string
	HTTPClient   *http.Client
}

func NewMediaService(cfg GraphConfig) (igdomain.MediaService, error) {
	client, err := meta.NewClient(meta.Config{
		Host:       GraphHost,
		APIVersion: graphVersionOr(cfg.GraphVersion),
		AppSecret:  cfg.AppSecret,
		HTTPClient: cfg.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	return &mediaService{client: client}, nil
}

type paging struct {
	Cursors struct {
		Before string `json:"before"`
		After  string `json:"after"`
	} `json:"cursors"`
	Next     string `json:"next"`
	Previous string `json:"previous"`
}

type rawMedia struct {
	ID               string `json:"id"`
	Caption          string `json:"caption"`
	MediaType        string `json:"media_type"`
	MediaProductType string `json:"media_product_type"`
	MediaURL         string `json:"media_url"`
	Permalink        string `json:"permalink"`
	ThumbnailURL     string `json:"thumbnail_url"`
	Timestamp        string `json:"timestamp"`
	Username         string `json:"username"`
	LikeCount        int    `json:"like_count"`
	CommentsCount    int    `json:"comments_count"`
	IsCommentEnabled *bool  `json:"is_comment_enabled"`
	Shortcode        string `json:"shortcode"`

	Children *struct {
		Data []*rawMedia `json:"data"`
	} `json:"children"`
}

func (r *rawMedia) toDomain() *igdomain.RemoteMedia {
	m := &igdomain.RemoteMedia{
		IGMediaID:        r.ID,
		MediaType:        igdomain.MediaType(strings.ToUpper(r.MediaType)),
		MediaProductType: igdomain.MediaProductType(strings.ToUpper(r.MediaProductType)),
		Caption:          r.Caption,
		Permalink:        r.Permalink,
		Shortcode:        r.Shortcode,
		Username:         r.Username,
		LikeCount:        r.LikeCount,
		CommentsCount:    r.CommentsCount,
		IsCommentEnabled: r.IsCommentEnabled,
		MediaURL:         r.MediaURL,
		ThumbnailURL:     r.ThumbnailURL,
	}
	if ts := parseGraphTime(r.Timestamp); ts != nil {
		m.Timestamp = ts
	}
	if r.Children != nil {
		for _, c := range r.Children.Data {
			if c != nil {
				m.Children = append(m.Children, c.toDomain())
			}
		}
	}
	return m
}

type mediaListResponse struct {
	Data   []*rawMedia `json:"data"`
	Paging paging      `json:"paging"`
}

func (s *mediaService) ListMedia(ctx context.Context, igUserID, token string, limit int, after string) (*igdomain.Page[*igdomain.RemoteMedia], error) {
	q := url.Values{}
	q.Set("fields", strings.Join(igdomain.MediaFields(), ",")+
		",children{id,media_type,media_product_type,media_url,thumbnail_url,permalink}")
	if limit > 0 {
		q.Set("limit", fmt.Sprint(limit))
	}
	if after != "" {
		q.Set("after", after)
	}

	var out mediaListResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodGet,
		Path:   "/" + igUserID + "/media",
		Token:  token,
		Query:  q,
	}, &out); err != nil {
		return nil, err
	}

	items := make([]*igdomain.RemoteMedia, 0, len(out.Data))
	for _, r := range out.Data {
		if r != nil {
			items = append(items, r.toDomain())
		}
	}
	return &igdomain.Page[*igdomain.RemoteMedia]{
		Items:      items,
		NextCursor: out.Paging.Cursors.After,
		PrevCursor: out.Paging.Cursors.Before,
		HasNext:    out.Paging.Next != "",
	}, nil
}

func (s *mediaService) GetMedia(ctx context.Context, token, igMediaID string, withChildren bool) (*igdomain.RemoteMedia, error) {
	fields := strings.Join(igdomain.MediaFields(), ",")
	if withChildren {
		fields += ",children{id,media_type,media_product_type,media_url,thumbnail_url,permalink}"
	}
	q := url.Values{}
	q.Set("fields", fields)

	var out rawMedia
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodGet,
		Path:   "/" + igMediaID,
		Token:  token,
		Query:  q,
	}, &out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, igdomain.ErrMediaNotFound
	}
	return out.toDomain(), nil
}

type containerResponse struct {
	ID string `json:"id"`
}

func (s *mediaService) CreateContainer(ctx context.Context, igUserID, token string, in igdomain.CreateMediaInput) (string, error) {
	form := url.Values{}
	switch {
	case strings.TrimSpace(in.ImageURL) != "":
		form.Set("image_url", in.ImageURL)
	case strings.TrimSpace(in.VideoURL) != "":
		form.Set("video_url", in.VideoURL)
	default:
		return "", fmt.Errorf("instagram: publishing requires image_url or video_url")
	}
	if in.Caption != "" {
		form.Set("caption", in.Caption)
	}
	if in.MediaType != "" {
		form.Set("media_type", strings.ToUpper(in.MediaType))
	}

	var out containerResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodPost,
		Path:   "/" + igUserID + "/media",
		Token:  token,
		Form:   form,
	}, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("instagram: container creation returned no id")
	}
	return out.ID, nil
}

type containerStatusResponse struct {
	ID         string `json:"id"`
	StatusCode string `json:"status_code"`
	Status     string `json:"status"`
}

func (s *mediaService) GetContainerStatus(ctx context.Context, token, containerID string) (*igdomain.ContainerStatus, error) {
	q := url.Values{}
	q.Set("fields", "id,status_code,status")

	var out containerStatusResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodGet,
		Path:   "/" + containerID,
		Token:  token,
		Query:  q,
	}, &out); err != nil {
		return nil, err
	}
	return &igdomain.ContainerStatus{
		ID:         out.ID,
		StatusCode: strings.ToUpper(out.StatusCode),
		Status:     out.Status,
	}, nil
}

func (s *mediaService) PublishContainer(ctx context.Context, igUserID, token, containerID string) (string, error) {
	form := url.Values{}
	form.Set("creation_id", containerID)

	var out containerResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodPost,
		Path:   "/" + igUserID + "/media_publish",
		Token:  token,
		Form:   form,
	}, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("instagram: publish returned no media id")
	}
	return out.ID, nil
}

func (s *mediaService) SetCommentEnabled(ctx context.Context, token, igMediaID string, enabled bool) error {
	form := url.Values{}
	form.Set("comment_enabled", fmt.Sprint(enabled))

	return s.client.Do(ctx, meta.Request{
		Method: http.MethodPost,
		Path:   "/" + igMediaID,
		Token:  token,
		Form:   form,
	}, nil)
}

func (s *mediaService) FetchMediaBytes(ctx context.Context, rawURL string) ([]byte, string, error) {
	return s.client.FetchBytes(ctx, rawURL)
}

func parseGraphTime(v string) *time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05-0700",
		time.RFC3339,
		"2006-01-02T15:04:05+0000",
	} {
		if t, err := time.Parse(layout, v); err == nil {
			utc := t.UTC()
			return &utc
		}
	}
	return nil
}

func graphVersionOr(v string) string {
	if strings.TrimSpace(v) == "" {
		return DefaultGraphVersion
	}
	return v
}
