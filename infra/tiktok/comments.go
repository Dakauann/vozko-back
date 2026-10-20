package tiktok

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	ttdomain "vozko/domain/tiktok"
)

const commentListMax = 30

type wireComment struct {
	CommentID        ttdomain.NumericID `json:"comment_id"`
	VideoID          ttdomain.NumericID `json:"video_id"`
	ParentCommentID  ttdomain.NumericID `json:"parent_comment_id"`
	UniqueIdentifier string             `json:"unique_identifier"`
	Username         string             `json:"username"`
	DisplayName      string             `json:"display_name"`
	ProfileImage     string             `json:"profile_image"`
	Text             string             `json:"text"`
	ImageURL         string             `json:"image_url"`
	Status           string             `json:"status"`
	Owner            bool               `json:"owner"`
	Liked            bool               `json:"liked"`
	Likes            int                `json:"likes"`
	Replies          int                `json:"replies"`
	CreateTime       json.Number        `json:"create_time"`
	ReplyList        []wireComment      `json:"reply_list"`
}

func (w wireComment) remote(parent string) ttdomain.RemoteComment {
	parentID := w.ParentCommentID.String()
	if parentID == "" {
		parentID = parent
	}
	return ttdomain.RemoteComment{
		CommentID:        w.CommentID.String(),
		VideoID:          w.VideoID.String(),
		ParentCommentID:  parentID,
		UniqueIdentifier: strings.TrimSpace(w.UniqueIdentifier),
		Username:         strings.TrimSpace(w.Username),
		DisplayName:      strings.TrimSpace(w.DisplayName),
		AvatarURL:        strings.TrimSpace(w.ProfileImage),
		Text:             w.Text,
		ImageURL:         strings.TrimSpace(w.ImageURL),
		Visibility:       ttdomain.VisibilityFromStatus(w.Status),
		Owner:            w.Owner,
		Liked:            w.Liked,
		Likes:            w.Likes,
		Replies:          w.Replies,
		CreatedAt:        unixSeconds(w.CreateTime),
	}
}

type commentListResponse struct {
	Comments []wireComment `json:"comments"`
	Cursor   int64         `json:"cursor"`
	HasMore  bool          `json:"has_more"`
}

func flatten(comments []wireComment) []ttdomain.RemoteComment {
	out := make([]ttdomain.RemoteComment, 0, len(comments))
	for _, c := range comments {
		if c.CommentID == "" {
			continue
		}
		out = append(out, c.remote(""))
		for _, r := range c.ReplyList {
			if r.CommentID != "" {
				out = append(out, r.remote(c.CommentID.String()))
			}
		}
	}
	return out
}

func (c *Client) ListComments(ctx context.Context, creds ttdomain.Credentials, query ttdomain.CommentQuery) (ttdomain.CommentPage, error) {
	limit := query.Limit
	if limit <= 0 || limit > commentListMax {
		limit = commentListMax
	}
	q := url.Values{}
	q.Set("business_id", creds.OpenID)
	q.Set("video_id", query.VideoID)
	q.Set("max_count", strconv.Itoa(limit))
	q.Set("sort_field", "create_time")
	q.Set("sort_order", "desc")
	q.Set("status", "PUBLIC")
	if query.IncludeHidden {
		q.Set("status", "ALL")
	}
	if query.IncludeReplies {
		q.Set("include_replies", "true")
	}
	if query.Cursor > 0 {
		q.Set("cursor", strconv.FormatInt(query.Cursor, 10))
	}
	var out commentListResponse
	if err := c.getJSON(ctx, request{path: "/business/comment/list/", query: q, family: FamilyCommentList, creds: &creds}, &out); err != nil {
		return ttdomain.CommentPage{}, err
	}
	return ttdomain.CommentPage{Comments: flatten(out.Comments), Cursor: out.Cursor, HasMore: out.HasMore}, nil
}

func (c *Client) LookupComments(ctx context.Context, creds ttdomain.Credentials, videoID string, commentIDs []string) ([]ttdomain.RemoteComment, error) {
	ids := make([]string, 0, len(commentIDs))
	for _, id := range commentIDs {
		if ttdomain.ValidNumericID(id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > ttdomain.CommentLookupBatch {
		ids = ids[:ttdomain.CommentLookupBatch]
	}
	q := url.Values{}
	q.Set("business_id", creds.OpenID)
	q.Set("video_id", videoID)
	q.Set("comment_ids", jsonList(ids))
	q.Set("status", "ALL")
	q.Set("max_count", strconv.Itoa(commentListMax))
	var out commentListResponse
	if err := c.getJSON(ctx, request{path: "/business/comment/list/", query: q, family: FamilyCommentList, creds: &creds}, &out); err != nil {
		return nil, err
	}
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	found := make([]ttdomain.RemoteComment, 0, len(ids))
	for _, rc := range flatten(out.Comments) {
		if _, ok := wanted[rc.CommentID]; ok {
			found = append(found, rc)
		}
	}
	return found, nil
}

func (c *Client) Reply(ctx context.Context, creds ttdomain.Credentials, videoID, commentID, text string) (string, error) {
	var out struct {
		CommentID ttdomain.NumericID `json:"comment_id"`
	}
	err := c.postJSON(ctx, request{path: "/business/comment/reply/create/", family: FamilyCommentReply, creds: &creds}, map[string]string{
		"business_id": creds.OpenID,
		"video_id":    videoID,
		"comment_id":  commentID,
		"text":        text,
	}, &out)
	if err != nil {
		return "", err
	}
	if out.CommentID == "" {
		return "", &Error{Code: CodeDecode, Message: "tiktok accepted the reply without returning its id", Ambiguous: true}
	}
	return out.CommentID.String(), nil
}

func (c *Client) SetHidden(ctx context.Context, creds ttdomain.Credentials, videoID, commentID string, hidden bool) error {
	action := "UNHIDE"
	if hidden {
		action = "HIDE"
	}
	return c.postJSON(ctx, request{path: "/business/comment/hide/", family: FamilyCommentHide, creds: &creds, retrySafe: true}, map[string]string{
		"business_id": creds.OpenID,
		"video_id":    videoID,
		"comment_id":  commentID,
		"action":      action,
	}, nil)
}

func (c *Client) SetLiked(ctx context.Context, creds ttdomain.Credentials, commentID string, liked bool) error {
	action := "UNLIKE"
	if liked {
		action = "LIKE"
	}
	return c.postJSON(ctx, request{path: "/business/comment/like/", family: FamilyCommentLike, creds: &creds, retrySafe: true}, map[string]string{
		"business_id": creds.OpenID,
		"comment_id":  commentID,
		"action":      action,
	}, nil)
}
