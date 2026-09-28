package facebook

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta"
)

const commentFields = "id,message,created_time,from{id,name},parent{id},like_count,comment_count,is_hidden," +
	"can_hide,can_remove,can_reply_privately,can_like,user_likes,attachment{type,url}"

type commentService struct {
	client *meta.Client
}

func NewCommentService(cfg GraphConfig) (fbdomain.CommentService, error) {
	client, err := newGraphClient(cfg, GraphHost)
	if err != nil {
		return nil, err
	}
	return &commentService{client: client}, nil
}

type graphComment struct {
	ID          string `json:"id"`
	Message     string `json:"message"`
	CreatedTime string `json:"created_time"`
	From        *struct {
		ID   meta.GraphID `json:"id"`
		Name string       `json:"name"`
	} `json:"from"`
	Parent *struct {
		ID string `json:"id"`
	} `json:"parent"`
	LikeCount         int  `json:"like_count"`
	CommentCount      int  `json:"comment_count"`
	IsHidden          bool `json:"is_hidden"`
	CanHide           bool `json:"can_hide"`
	CanRemove         bool `json:"can_remove"`
	CanReplyPrivately bool `json:"can_reply_privately"`
	CanLike           bool `json:"can_like"`
	UserLikes         bool `json:"user_likes"`
	Attachment        *struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"attachment"`
}

func (s *commentService) List(ctx context.Context, pageToken, fbObjectID string, filter fbdomain.CommentFilter, limit int, after string) (*fbdomain.Paged[*fbdomain.RemoteComment], error) {
	q := url.Values{}
	q.Set("fields", commentFields)
	q.Set("filter", string(filter))
	q.Set("order", "reverse_chronological")
	q.Set("limit", strconv.Itoa(limit))
	if after != "" {
		q.Set("after", after)
	}
	var out struct {
		Data   []graphComment `json:"data"`
		Paging struct {
			Cursors struct {
				After string `json:"after"`
			} `json:"cursors"`
			Next string `json:"next"`
		} `json:"paging"`
	}
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: "/" + fbObjectID + "/comments", Token: pageToken, Query: q}, &out); err != nil {
		return nil, err
	}
	page := &fbdomain.Paged[*fbdomain.RemoteComment]{Items: make([]*fbdomain.RemoteComment, 0, len(out.Data))}
	for i := range out.Data {
		page.Items = append(page.Items, remoteCommentOf(&out.Data[i]))
	}
	if out.Paging.Next != "" {
		page.NextCursor, page.HasNext = out.Paging.Cursors.After, true
	}
	return page, nil
}

func (s *commentService) Get(ctx context.Context, pageToken, fbCommentID string) (*fbdomain.RemoteComment, error) {
	q := url.Values{}
	q.Set("fields", commentFields)
	var out graphComment
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: "/" + fbCommentID, Token: pageToken, Query: q}, &out); err != nil {
		return nil, err
	}
	return remoteCommentOf(&out), nil
}

func (s *commentService) Create(ctx context.Context, pageToken, fbParentID, message string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodPost, Path: "/" + fbParentID + "/comments", Token: pageToken, Body: map[string]string{"message": message},
	}, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("facebook: comment created without an id")
	}
	return out.ID, nil
}

func (s *commentService) Edit(ctx context.Context, pageToken, fbCommentID, message string) error {
	return acknowledged(ctx, s.client, meta.Request{
		Method: http.MethodPost, Path: "/" + fbCommentID, Token: pageToken, Body: map[string]string{"message": message}, Idempotent: true,
	})
}

func (s *commentService) SetHidden(ctx context.Context, pageToken, fbCommentID string, hidden bool) error {
	return acknowledged(ctx, s.client, meta.Request{
		Method: http.MethodPost, Path: "/" + fbCommentID, Token: pageToken, Body: map[string]bool{"is_hidden": hidden}, Idempotent: true,
	})
}

func (s *commentService) Delete(ctx context.Context, pageToken, fbCommentID string) error {
	return acknowledged(ctx, s.client, meta.Request{Method: http.MethodDelete, Path: "/" + fbCommentID, Token: pageToken})
}

func (s *commentService) SetLiked(ctx context.Context, pageToken, fbCommentID string, liked bool) error {
	method := http.MethodDelete
	if liked {
		method = http.MethodPost
	}
	return acknowledged(ctx, s.client, meta.Request{Method: method, Path: "/" + fbCommentID + "/likes", Token: pageToken, Idempotent: true})
}

func remoteCommentOf(c *graphComment) *fbdomain.RemoteComment {
	out := &fbdomain.RemoteComment{
		FBCommentID:       c.ID,
		Message:           c.Message,
		CreatedTime:       graphTime(c.CreatedTime),
		LikeCount:         c.LikeCount,
		CommentCount:      c.CommentCount,
		IsHidden:          c.IsHidden,
		UserLikes:         c.UserLikes,
		CanHide:           c.CanHide,
		CanRemove:         c.CanRemove,
		CanReplyPrivately: c.CanReplyPrivately,
		CanLike:           c.CanLike,
	}
	if c.From != nil {
		out.FromID, out.FromName = c.From.ID.String(), c.From.Name
	}
	if c.Parent != nil {
		out.ParentID = c.Parent.ID
	}
	if c.Attachment != nil {
		out.AttachmentType, out.AttachmentURL = c.Attachment.Type, c.Attachment.URL
	}
	return out
}
