package instagram

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	igdomain "vozko/domain/instagram"
	"vozko/infra/meta"
)

// maxCommentsPerPage is Graph's documented ceiling: "Returns a maximum of 50
// comments per query".
const maxCommentsPerPage = 50

type commentService struct {
	client *meta.Client
}

// NewCommentService builds the comment moderation client.
func NewCommentService(cfg GraphConfig) (igdomain.CommentService, error) {
	client, err := meta.NewClient(meta.Config{
		Host:       GraphHost,
		APIVersion: graphVersionOr(cfg.GraphVersion),
		AppSecret:  cfg.AppSecret,
		HTTPClient: cfg.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	return &commentService{client: client}, nil
}

// rawComment mirrors the IG Comment node.
//
// `From` is the public commenter and is what the CRM keys on. `User` is
// populated ONLY when our own app user authored the comment, which is how we
// know whether deletion is even possible: DELETE requires the comment creator's
// token, so we can delete our replies but must hide anyone else's.
type rawComment struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
	Username  string `json:"username"`
	LikeCount int    `json:"like_count"`
	Hidden    bool   `json:"hidden"`
	ParentID  string `json:"parent_id"`

	From *struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"from"`

	User *struct {
		ID string `json:"id"`
	} `json:"user"`

	Replies *struct {
		Data []*rawComment `json:"data"`
	} `json:"replies"`
}

func (r *rawComment) toDomain() *igdomain.RemoteComment {
	c := &igdomain.RemoteComment{
		IGCommentID:  r.ID,
		Text:         r.Text,
		Timestamp:    parseGraphTime(r.Timestamp),
		Username:     r.Username,
		LikeCount:    r.LikeCount,
		Hidden:       r.Hidden,
		ParentID:     r.ParentID,
		IsOurs:       r.User != nil && r.User.ID != "",
		FromUsername: r.Username,
	}
	if r.From != nil {
		c.FromIGSID = r.From.ID
		if r.From.Username != "" {
			c.FromUsername = r.From.Username
		}
	}
	if r.Replies != nil {
		for _, rep := range r.Replies.Data {
			if rep != nil {
				c.Replies = append(c.Replies, rep.toDomain())
			}
		}
	}
	return c
}

type commentListResponse struct {
	Data   []*rawComment `json:"data"`
	Paging paging        `json:"paging"`
}

// ListComments returns top-level comments with their replies expanded.
//
// The edge returns only top-level comments unless `replies` is field-expanded,
// is reverse-chronological, and cannot be filtered by timestamp — which is why
// incremental sync comes from webhooks rather than polling.
func (s *commentService) ListComments(ctx context.Context, token, igMediaID string, limit int, after string) (*igdomain.Page[*igdomain.RemoteComment], error) {
	if limit <= 0 || limit > maxCommentsPerPage {
		limit = maxCommentsPerPage
	}

	q := url.Values{}
	q.Set("fields", strings.Join(igdomain.CommentFields(), ",")+
		",replies{"+strings.Join(igdomain.CommentFields(), ",")+"}")
	q.Set("limit", fmt.Sprint(limit))
	if after != "" {
		q.Set("after", after)
	}

	var out commentListResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodGet,
		Path:   "/" + igMediaID + "/comments",
		Token:  token,
		Query:  q,
	}, &out); err != nil {
		return nil, err
	}

	return toCommentPage(out), nil
}

func (s *commentService) ListReplies(ctx context.Context, token, igCommentID string, limit int, after string) (*igdomain.Page[*igdomain.RemoteComment], error) {
	if limit <= 0 || limit > maxCommentsPerPage {
		limit = maxCommentsPerPage
	}

	q := url.Values{}
	q.Set("fields", strings.Join(igdomain.CommentFields(), ","))
	q.Set("limit", fmt.Sprint(limit))
	if after != "" {
		q.Set("after", after)
	}

	var out commentListResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodGet,
		Path:   "/" + igCommentID + "/replies",
		Token:  token,
		Query:  q,
	}, &out); err != nil {
		return nil, err
	}
	return toCommentPage(out), nil
}

func (s *commentService) GetComment(ctx context.Context, token, igCommentID string) (*igdomain.RemoteComment, error) {
	q := url.Values{}
	q.Set("fields", strings.Join(igdomain.CommentFields(), ","))

	var out rawComment
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodGet,
		Path:   "/" + igCommentID,
		Token:  token,
		Query:  q,
	}, &out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, igdomain.ErrCommentNotFound
	}
	return out.toDomain(), nil
}

type createdCommentResponse struct {
	ID string `json:"id"`
}

func (s *commentService) ReplyToComment(ctx context.Context, token, igCommentID, message string) (string, error) {
	form := url.Values{}
	form.Set("message", message)

	var out createdCommentResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodPost,
		Path:   "/" + igCommentID + "/replies",
		Token:  token,
		Form:   form,
	}, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (s *commentService) CreateComment(ctx context.Context, token, igMediaID, message string) (string, error) {
	form := url.Values{}
	form.Set("message", message)

	var out createdCommentResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodPost,
		Path:   "/" + igMediaID + "/comments",
		Token:  token,
		Form:   form,
	}, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// SetHidden hides or unhides a comment. This is the moderation action for
// someone else's comment and requires the media owner's token.
func (s *commentService) SetHidden(ctx context.Context, token, igCommentID string, hidden bool) error {
	form := url.Values{}
	form.Set("hide", fmt.Sprint(hidden))

	return s.client.Do(ctx, meta.Request{
		Method: http.MethodPost,
		Path:   "/" + igCommentID,
		Token:  token,
		Form:   form,
	}, nil)
}

// Delete removes a comment. Graph requires a token from the COMMENT CREATOR, so
// in practice this succeeds only for replies we authored; the usecase checks
// IsOurs before calling.
func (s *commentService) Delete(ctx context.Context, token, igCommentID string) error {
	return s.client.Do(ctx, meta.Request{
		Method: http.MethodDelete,
		Path:   "/" + igCommentID,
		Token:  token,
	}, nil)
}

func toCommentPage(out commentListResponse) *igdomain.Page[*igdomain.RemoteComment] {
	items := make([]*igdomain.RemoteComment, 0, len(out.Data))
	for _, r := range out.Data {
		if r != nil {
			items = append(items, r.toDomain())
		}
	}
	return &igdomain.Page[*igdomain.RemoteComment]{
		Items:      items,
		NextCursor: out.Paging.Cursors.After,
		PrevCursor: out.Paging.Cursors.Before,
		HasNext:    out.Paging.Next != "",
	}
}
