package advertising

import (
	"errors"
	"slices"
	"strings"
	"time"
)

var (
	ErrCommentsOnlyForAds    = errors.New("only ads have comments")
	ErrUnknownCommentChannel = errors.New("comments are read from facebook or instagram")
	ErrAdHasNoPost           = errors.New("meta has not created the ad's post on this platform yet")
	ErrCommentsNotAllowed    = errors.New("this meta connection is not allowed to read the comments")
	ErrCommentsNeedReconnect = errors.New("the ads connection lacks the permission to read comments; reconnect it")
)

const (
	ScopePagesReadUserContent    = "pages_read_user_content"
	ScopeInstagramBasic          = "instagram_basic"
	ScopeInstagramManageComments = "instagram_manage_comments"
)

var commentScopes = map[string][]string{
	PlatformFacebook:  {ScopePagesReadEngage, ScopePagesReadUserContent},
	PlatformInstagram: {ScopeInstagramBasic, ScopeInstagramManageComments},
}

func (g *Grant) CanReadComments(platform string) error {
	for _, scope := range commentScopes[platform] {
		if g == nil || !slices.Contains(g.Scopes, scope) {
			return ErrCommentsNeedReconnect
		}
	}
	return nil
}

const MaxAdComments = 100

type AdComment struct {
	ID         string
	Message    string
	AuthorName string
	CreatedAt  *time.Time
	LikeCount  int
	ReplyCount int
}

type AdPosts struct {
	FacebookPostID   string
	InstagramMediaID string
}

func CommentPlatformOf(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", PlatformFacebook:
		return PlatformFacebook, nil
	case PlatformInstagram:
		return PlatformInstagram, nil
	}
	return "", ErrUnknownCommentChannel
}

func (p AdPosts) PostOn(platform string) (string, error) {
	id := p.FacebookPostID
	if platform == PlatformInstagram {
		id = p.InstagramMediaID
	}
	if id == "" || (platform == PlatformFacebook && p.PageID() == "") {
		return "", ErrAdHasNoPost
	}
	return id, nil
}

func (p AdPosts) PageID() string {
	page, _, found := strings.Cut(p.FacebookPostID, "_")
	if !found {
		return ""
	}
	return page
}

func (o *Object) CommentsAllowed() error {
	if o.Level != LevelAd {
		return ErrCommentsOnlyForAds
	}
	return nil
}
