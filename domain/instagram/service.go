package instagram

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type TokenGrant struct {
	AccessToken string
	ExpiresIn   time.Duration
	UserID      string
	Permissions []string
}

type Profile struct {
	IGUserID          string
	Username          string
	Name              string
	AccountType       string
	ProfilePictureURL string
	FollowersCount    int
	FollowsCount      int
	MediaCount        int
}

type OAuthService interface {
	BuildAuthorizeURL(state string) string
	ExchangeCode(ctx context.Context, code string) (*TokenGrant, error)
	ExchangeForLongLived(ctx context.Context, shortLivedToken string) (*TokenGrant, error)
	RefreshToken(ctx context.Context, longLivedToken string) (*TokenGrant, error)
	GetProfile(ctx context.Context, token string) (*Profile, error)
}

type SubscriptionService interface {
	Subscribe(ctx context.Context, igUserID, token string, fields []string) error
	Unsubscribe(ctx context.Context, igUserID, token string) error
}

func SubscribedFields() []string {
	return []string{
		"messages",
		"message_reactions",
		"message_edit",
		"messaging_seen",
		"messaging_postbacks",
		"messaging_referral",
		"messaging_optins",
		"messaging_handover",
		"standby",

		"comments",
		"live_comments",
		"mentions",
	}
}

var validSubscribedFields = map[string]struct{}{
	"agent_messages": {}, "messages": {}, "messaging_postbacks": {},
	"messaging_seen": {}, "messaging_handover": {}, "messaging_referral": {},
	"messaging_optins": {}, "message_reactions": {}, "message_edit": {},
	"standby": {}, "comments": {}, "live_comments": {}, "mentions": {},
	"story_insights": {}, "creator_marketplace_projects": {},
	"creator_marketplace_invited_creator_onboarding": {}, "delta": {},
	"story_reactions": {}, "onboarding_welcome_message_series": {},
	"follow": {}, "comment_poll_response": {}, "story_poll_response": {},
	"share_to_story": {},
}

func InvalidSubscribedFields(fields []string) []string {
	var bad []string
	for _, f := range fields {
		if _, ok := validSubscribedFields[f]; !ok {
			bad = append(bad, f)
		}
	}
	return bad
}

type SendTextInput struct {
	RecipientIGSID string
	Text           string
	ReplyToMID     string

	QuickReplies []QuickReplyOption
}

type QuickReplyOption struct {
	Title   string
	Payload string
}

type SendMediaInput struct {
	RecipientIGSID string
	Kind           string
	URL            string
	ReplyToMID     string
}

type SendResult struct {
	RecipientID string
	MessageID   string
}

type ContactProfileResult struct {
	Username             string
	Name                 string
	ProfilePictureURL    string
	IsVerifiedUser       bool
	FollowerCount        int
	IsUserFollowBusiness bool
	IsBusinessFollowUser bool
}

type MessagingService interface {
	SendText(ctx context.Context, igUserID, token string, in SendTextInput) (*SendResult, error)
	SendMedia(ctx context.Context, igUserID, token string, in SendMediaInput) (*SendResult, error)

	SendReaction(ctx context.Context, igUserID, token, recipientIGSID, targetMID, reaction string) error
	RemoveReaction(ctx context.Context, igUserID, token, recipientIGSID, targetMID string) error

	SendTyping(ctx context.Context, igUserID, token, recipientIGSID string, on bool) error
	MarkSeen(ctx context.Context, igUserID, token, recipientIGSID string) error

	SendPrivateReply(ctx context.Context, igUserID, token, igCommentID, text string) (*SendResult, error)

	GetContactProfile(ctx context.Context, token, igsid string) (*ContactProfileResult, error)

	GetConversations(ctx context.Context, igUserID, token string, limit int) error
}

type Page[T any] struct {
	Items      []T
	NextCursor string
	PrevCursor string
	HasNext    bool
}

func MediaFields() []string {
	return []string{
		"id", "caption", "media_type", "media_product_type", "media_url",
		"permalink", "thumbnail_url", "timestamp", "username", "like_count",
		"comments_count", "is_comment_enabled", "shortcode",
	}
}

type RemoteMedia struct {
	IGMediaID        string
	MediaType        MediaType
	MediaProductType MediaProductType
	Caption          string
	Permalink        string
	Shortcode        string
	Timestamp        *time.Time
	Username         string
	LikeCount        int
	CommentsCount    int
	IsCommentEnabled *bool

	MediaURL     string
	ThumbnailURL string

	Children []*RemoteMedia
}

type CreateMediaInput struct {
	ImageURL  string
	VideoURL  string
	Caption   string
	MediaType string
}

type ContainerStatus struct {
	ID         string
	StatusCode string
	Status     string
}

func (c ContainerStatus) Ready() bool  { return c.StatusCode == "FINISHED" }
func (c ContainerStatus) Failed() bool { return c.StatusCode == "ERROR" || c.StatusCode == "EXPIRED" }

type MediaService interface {
	ListMedia(ctx context.Context, igUserID, token string, limit int, after string) (*Page[*RemoteMedia], error)
	GetMedia(ctx context.Context, token, igMediaID string, withChildren bool) (*RemoteMedia, error)

	CreateContainer(ctx context.Context, igUserID, token string, in CreateMediaInput) (containerID string, err error)
	GetContainerStatus(ctx context.Context, token, containerID string) (*ContainerStatus, error)
	PublishContainer(ctx context.Context, igUserID, token, containerID string) (igMediaID string, err error)

	SetCommentEnabled(ctx context.Context, token, igMediaID string, enabled bool) error

	FetchMediaBytes(ctx context.Context, url string) (data []byte, contentType string, err error)
}

func CommentFields() []string {
	return []string{
		"id", "text", "timestamp", "username", "from", "like_count",
		"hidden", "parent_id", "user",
	}
}

type RemoteComment struct {
	IGCommentID  string
	Text         string
	Timestamp    *time.Time
	Username     string
	FromIGSID    string
	FromUsername string
	LikeCount    int
	Hidden       bool
	ParentID     string
	IsOurs       bool
	Replies      []*RemoteComment
}

type CommentService interface {
	ListComments(ctx context.Context, token, igMediaID string, limit int, after string) (*Page[*RemoteComment], error)
	GetComment(ctx context.Context, token, igCommentID string) (*RemoteComment, error)
	ListReplies(ctx context.Context, token, igCommentID string, limit int, after string) (*Page[*RemoteComment], error)

	ReplyToComment(ctx context.Context, token, igCommentID, message string) (newIGCommentID string, err error)
	CreateComment(ctx context.Context, token, igMediaID, message string) (newIGCommentID string, err error)

	SetHidden(ctx context.Context, token, igCommentID string, hidden bool) error
	Delete(ctx context.Context, token, igCommentID string) error
}

func RequiredScopes() []string {
	return []string{
		ScopeBasic,
		ScopeManageMessages,
		ScopeManageComments,
		ScopeContentPublish,
	}
}

const (
	OAuthStartPath    = "/oauth/instagram/start"
	OAuthCallbackPath = "/oauth/instagram/callback"
)

func ValidateRedirectURI(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("instagram: redirect URI is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("instagram: redirect URI %q is not a valid URL: %w", raw, err)
	}
	if parsed.Host == "" {
		return fmt.Errorf("instagram: redirect URI %q must be absolute (scheme + host)", raw)
	}

	isLocal := strings.HasPrefix(parsed.Hostname(), "localhost") || parsed.Hostname() == "127.0.0.1"
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLocal) {
		return fmt.Errorf("instagram: redirect URI %q must use https (http is only allowed on localhost)", raw)
	}

	if strings.TrimSuffix(parsed.Path, "/") != OAuthCallbackPath {
		return fmt.Errorf(
			"instagram: redirect URI path is %q but this build serves %q, the path is "+
				"owned by the code, so only the host is configurable (the full URI must "+
				"also be registered in the App Dashboard under Instagram > API setup with "+
				"Instagram login > Set up Instagram business login > Redirect URL)",
			parsed.Path, OAuthCallbackPath)
	}

	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("instagram: redirect URI %q must not carry a query string or fragment", raw)
	}
	return nil
}

func RedirectURIFor(apiBaseURL string) string {
	return strings.TrimRight(strings.TrimSpace(apiBaseURL), "/") + OAuthCallbackPath
}
