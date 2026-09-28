package facebook

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta"
)

const postFields = "id,from{id},message,story,status_type,created_time,updated_time,permalink_url,full_picture," +
	"is_published,is_hidden,scheduled_publish_time,shares," +
	"attachments{media_type,type,url,title,description,media}," +
	"reactions.summary(total_count).limit(0),comments.summary(total_count).limit(0)"

const graphTimeLayout = "2006-01-02T15:04:05-0700"

var listEdges = map[fbdomain.PostListKind]string{
	fbdomain.ListPublished: "published_posts",
	fbdomain.ListScheduled: "scheduled_posts",
}

type postService struct {
	client *meta.Client
	video  *meta.Client
}

func NewPostService(cfg GraphConfig) (fbdomain.PostService, error) {
	client, err := newGraphClient(cfg, GraphHost)
	if err != nil {
		return nil, err
	}
	video, err := newGraphClient(cfg, GraphVideoHost)
	if err != nil {
		return nil, err
	}
	return &postService{client: client, video: video}, nil
}

type graphCount struct {
	Count int `json:"count"`
}

type graphSummary struct {
	Summary struct {
		TotalCount int `json:"total_count"`
	} `json:"summary"`
}

type graphAttachment struct {
	MediaType   string `json:"media_type"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Media       *struct {
		Image *struct {
			Src string `json:"src"`
		} `json:"image"`
	} `json:"media"`
}

type graphPost struct {
	ID   string `json:"id"`
	From *struct {
		ID meta.GraphID `json:"id"`
	} `json:"from"`
	Message              string       `json:"message"`
	Story                string       `json:"story"`
	StatusType           string       `json:"status_type"`
	CreatedTime          string       `json:"created_time"`
	UpdatedTime          string       `json:"updated_time"`
	PermalinkURL         string       `json:"permalink_url"`
	FullPicture          string       `json:"full_picture"`
	IsPublished          *bool        `json:"is_published"`
	IsHidden             bool         `json:"is_hidden"`
	ScheduledPublishTime int64        `json:"scheduled_publish_time"`
	Shares               *graphCount  `json:"shares"`
	Reactions            graphSummary `json:"reactions"`
	Comments             graphSummary `json:"comments"`
	Attachments          struct {
		Data []graphAttachment `json:"data"`
	} `json:"attachments"`
}

type graphPostPage struct {
	Data   []graphPost `json:"data"`
	Paging struct {
		Cursors struct {
			After string `json:"after"`
		} `json:"cursors"`
		Next string `json:"next"`
	} `json:"paging"`
}

func (s *postService) List(ctx context.Context, fbPageID, pageToken string, kind fbdomain.PostListKind, limit int, after string) (*fbdomain.Paged[*fbdomain.RemotePost], error) {
	if kind == fbdomain.ListReels {
		return s.listReels(ctx, fbPageID, pageToken, limit, after)
	}
	edge, ok := listEdges[kind]
	if !ok {
		return nil, fmt.Errorf("facebook: unknown post list %q", kind)
	}
	q := url.Values{}
	q.Set("fields", postFields)
	q.Set("limit", strconv.Itoa(limit))
	if after != "" {
		q.Set("after", after)
	}
	var out graphPostPage
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: "/" + fbPageID + "/" + edge, Token: pageToken, Query: q}, &out); err != nil {
		return nil, err
	}
	page := &fbdomain.Paged[*fbdomain.RemotePost]{Items: make([]*fbdomain.RemotePost, 0, len(out.Data))}
	for i := range out.Data {
		page.Items = append(page.Items, remotePostOf(&out.Data[i]))
	}
	if out.Paging.Next != "" {
		page.NextCursor, page.HasNext = out.Paging.Cursors.After, true
	}
	return page, nil
}

func (s *postService) Get(ctx context.Context, pageToken, fbPostID string) (*fbdomain.RemotePost, error) {
	q := url.Values{}
	q.Set("fields", postFields)
	var out graphPost
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: "/" + fbPostID, Token: pageToken, Query: q}, &out); err != nil {
		return nil, err
	}
	return remotePostOf(&out), nil
}

func (s *postService) CreateFeedPost(ctx context.Context, fbPageID, pageToken string, in fbdomain.FeedPostInput) (string, error) {
	body := map[string]any{}
	if in.Message != "" {
		body["message"] = in.Message
	}
	if in.Link != "" {
		body["link"] = in.Link
	}
	if len(in.AttachedMedia) > 0 {
		media := make([]map[string]string, 0, len(in.AttachedMedia))
		for _, id := range in.AttachedMedia {
			media = append(media, map[string]string{"media_fbid": id})
		}
		body["attached_media"] = media
	}
	if in.ScheduledAt != nil {
		body["published"] = false
		body["scheduled_publish_time"] = in.ScheduledAt.Unix()
		body["unpublished_content_type"] = "SCHEDULED"
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodPost, Path: "/" + fbPageID + "/feed", Token: pageToken, Body: body}, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("facebook: feed post created without an id")
	}
	return out.ID, nil
}

func (s *postService) UploadPhoto(ctx context.Context, fbPageID, pageToken string, in fbdomain.PhotoInput) (*fbdomain.PhotoResult, error) {
	body := map[string]any{"url": in.URL, "published": in.Published}
	if in.Caption != "" {
		body["caption"] = in.Caption
	}
	if in.Temporary {
		body["temporary"] = true
	}
	var out struct {
		ID     string `json:"id"`
		PostID string `json:"post_id"`
	}
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodPost, Path: "/" + fbPageID + "/photos", Token: pageToken, Body: body}, &out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, fmt.Errorf("facebook: photo uploaded without an id")
	}
	return &fbdomain.PhotoResult{PhotoID: out.ID, PostID: out.PostID}, nil
}

func (s *postService) Update(ctx context.Context, pageToken, fbPostID string, in fbdomain.PostUpdate) error {
	body := map[string]any{}
	if in.Message != nil {
		body["message"] = *in.Message
	}
	if in.IsHidden != nil {
		body["is_hidden"] = *in.IsHidden
	}
	if in.PublishNow {
		body["is_published"] = true
	}
	if in.ScheduledAt != nil {
		body["scheduled_publish_time"] = in.ScheduledAt.Unix()
	}
	return acknowledged(ctx, s.client, meta.Request{Method: http.MethodPost, Path: "/" + fbPostID, Token: pageToken, Body: body, Idempotent: true})
}

func (s *postService) Delete(ctx context.Context, pageToken, fbObjectID string) error {
	return acknowledged(ctx, s.client, meta.Request{Method: http.MethodDelete, Path: "/" + fbObjectID, Token: pageToken})
}

func (s *postService) FetchBytes(ctx context.Context, rawURL string) ([]byte, string, error) {
	return s.client.FetchBytes(ctx, rawURL)
}

func acknowledged(ctx context.Context, client *meta.Client, req meta.Request) error {
	var out struct {
		Success bool `json:"success"`
	}
	if err := client.Do(ctx, req, &out); err != nil {
		return err
	}
	if !out.Success {
		return fmt.Errorf("facebook: %s %s was not acknowledged", req.Method, req.Path)
	}
	return nil
}

func remotePostOf(p *graphPost) *fbdomain.RemotePost {
	out := &fbdomain.RemotePost{
		FBPostID:       p.ID,
		Message:        p.Message,
		Story:          p.Story,
		StatusType:     p.StatusType,
		PermalinkURL:   p.PermalinkURL,
		FullPicture:    p.FullPicture,
		CreatedTime:    graphTime(p.CreatedTime),
		UpdatedTime:    graphTime(p.UpdatedTime),
		IsPublished:    p.IsPublished == nil || *p.IsPublished,
		IsHidden:       p.IsHidden,
		ReactionsCount: p.Reactions.Summary.TotalCount,
		CommentsCount:  p.Comments.Summary.TotalCount,
	}
	if p.From != nil {
		out.FromID = p.From.ID.String()
	}
	if p.Shares != nil {
		out.SharesCount = p.Shares.Count
	}
	if p.ScheduledPublishTime > 0 {
		at := time.Unix(p.ScheduledPublishTime, 0).UTC()
		out.ScheduledPublishTime = &at
	}
	for _, a := range p.Attachments.Data {
		attachment := fbdomain.RemoteAttachment{Type: a.Type, MediaType: a.MediaType, URL: a.URL, Title: a.Title, Description: a.Description}
		if a.Media != nil && a.Media.Image != nil {
			attachment.ImageURL = a.Media.Image.Src
		}
		out.Attachments = append(out.Attachments, attachment)
	}
	return out
}

func graphTime(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	t, err := time.Parse(graphTimeLayout, raw)
	if err != nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}
