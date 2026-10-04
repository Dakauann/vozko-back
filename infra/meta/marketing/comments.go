package marketing

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

const (
	adPostFields           = "creative{effective_object_story_id,effective_instagram_media_id}"
	facebookCommentFields  = "id,message,from{name},created_time,like_count,comment_count"
	instagramCommentFields = "id,text,username,timestamp,like_count,replies.summary(true){id}"
)

type graphFacebookComment struct {
	ID   meta.GraphID `json:"id"`
	Text string       `json:"message"`
	From *struct {
		Name string `json:"name"`
	} `json:"from"`
	CreatedTime string `json:"created_time"`
	Likes       int    `json:"like_count"`
	Replies     int    `json:"comment_count"`
}

type graphInstagramComment struct {
	ID        meta.GraphID `json:"id"`
	Text      string       `json:"text"`
	Username  string       `json:"username"`
	Timestamp string       `json:"timestamp"`
	Likes     int          `json:"like_count"`
	Replies   *struct {
		Summary struct {
			TotalCount int `json:"total_count"`
		} `json:"summary"`
	} `json:"replies"`
}

func (c graphFacebookComment) comment() (advertising.AdComment, error) {
	created, err := graphTime("created_time", c.CreatedTime)
	if err != nil {
		return advertising.AdComment{}, err
	}
	comment := advertising.AdComment{ID: c.ID.String(), Message: c.Text, CreatedAt: created, LikeCount: c.Likes, ReplyCount: c.Replies}
	if c.From != nil {
		comment.AuthorName = c.From.Name
	}
	return comment, nil
}

func (c graphInstagramComment) comment() (advertising.AdComment, error) {
	created, err := graphTime("timestamp", c.Timestamp)
	if err != nil {
		return advertising.AdComment{}, err
	}
	comment := advertising.AdComment{ID: c.ID.String(), Message: c.Text, AuthorName: c.Username, CreatedAt: created, LikeCount: c.Likes}
	if c.Replies != nil {
		comment.ReplyCount = c.Replies.Summary.TotalCount
	}
	return comment, nil
}

func adComments[T interface {
	comment() (advertising.AdComment, error)
}](rows []T) ([]advertising.AdComment, error) {
	comments := make([]advertising.AdComment, 0, len(rows))
	for _, row := range rows {
		comment, err := row.comment()
		if err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	return comments, nil
}

func (g *Gateway) GetAdPosts(ctx context.Context, token, adMetaID string) (advertising.AdPosts, error) {
	path, err := objectPath(adMetaID)
	if err != nil {
		return advertising.AdPosts{}, err
	}
	var row struct {
		Creative *struct {
			StoryID meta.GraphID `json:"effective_object_story_id"`
			MediaID meta.GraphID `json:"effective_instagram_media_id"`
		} `json:"creative"`
	}
	if err := g.get(ctx, path, token, adPostFields, &row); err != nil {
		return advertising.AdPosts{}, err
	}
	if row.Creative == nil {
		return advertising.AdPosts{}, nil
	}
	return advertising.AdPosts{FacebookPostID: row.Creative.StoryID.String(), InstagramMediaID: row.Creative.MediaID.String()}, nil
}

func commentsQuery(fields string) url.Values {
	q := url.Values{}
	q.Set("fields", fields)
	q.Set("limit", strconv.Itoa(advertising.MaxAdComments))
	return q
}

func (g *Gateway) ListPostComments(ctx context.Context, token, pageID, postID string) ([]advertising.AdComment, error) {
	_, pageToken, err := g.pageToken(ctx, token, pageID)
	if err != nil {
		return nil, err
	}
	path, err := objectPath(postID)
	if err != nil {
		return nil, err
	}
	q := commentsQuery(facebookCommentFields)
	q.Set("order", "reverse_chronological")
	var page graphPage[graphFacebookComment]
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path + "/comments", Token: pageToken, Query: q}, &page); err != nil {
		return nil, err
	}
	return adComments(page.Data)
}

func (g *Gateway) ListInstagramComments(ctx context.Context, token, mediaID string) ([]advertising.AdComment, error) {
	path, err := objectPath(mediaID)
	if err != nil {
		return nil, err
	}
	var page graphPage[graphInstagramComment]
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path + "/comments", Token: token, Query: commentsQuery(instagramCommentFields)}, &page); err != nil {
		return nil, err
	}
	return adComments(page.Data)
}
