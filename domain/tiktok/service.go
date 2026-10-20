package tiktok

import (
	"context"
	"strings"
	"time"
)

type Grant struct {
	OpenID           string
	AccessToken      string
	RefreshToken     string
	ExpiresIn        time.Duration
	RefreshExpiresIn time.Duration
	Scopes           []string
}

func (g Grant) Validate() error {
	if !ValidOpaqueID(g.OpenID) || strings.TrimSpace(g.AccessToken) == "" || strings.TrimSpace(g.RefreshToken) == "" || g.ExpiresIn <= 0 {
		return ErrGrantIncomplete
	}
	return nil
}

type TokenInfo struct {
	OpenID string
	Scopes []string
}

type OAuthService interface {
	AuthorizeURL(state string) (string, error)
	ExchangeCode(ctx context.Context, code string) (Grant, error)
	Refresh(ctx context.Context, refreshToken string) (Grant, error)
	TokenInfo(ctx context.Context, accessToken string) (TokenInfo, error)
	Revoke(ctx context.Context, accessToken string) error
}

type BusinessProfile struct {
	Username          string
	DisplayName       string
	AvatarURL         string
	HasAdvancedAccess bool
	IsBusinessAccount bool
}

type ProfileService interface {
	Profile(ctx context.Context, creds Credentials) (BusinessProfile, error)
}

type SenderAction string

const (
	SenderTyping   SenderAction = "TYPING"
	SenderMarkRead SenderAction = "MARK_READ"
)

type MediaKind string

const (
	MediaKindImage MediaKind = "IMAGE"
	MediaKindVideo MediaKind = "VIDEO"
)

type MediaRef struct {
	ConversationID string
	MessageID      string
	MediaID        string
	Kind           MediaKind
}

type DownloadedMedia struct {
	Data     []byte
	MimeType string
}

type Participant struct {
	PersonID    string
	Username    string
	DisplayName string
	AvatarURL   string
}

type MessagingService interface {
	SendText(ctx context.Context, creds Credentials, conversationID, text, replyTo string) (string, error)
	SendImage(ctx context.Context, creds Credentials, conversationID, mediaID string) (string, error)
	SendCard(ctx context.Context, creds Credentials, conversationID string, card Card) (string, error)
	SendAction(ctx context.Context, creds Credentials, conversationID string, action SenderAction) error
	UploadImage(ctx context.Context, creds Credentials, data []byte, fileName, mimeType string) (string, error)
	DownloadMedia(ctx context.Context, creds Credentials, ref MediaRef) (DownloadedMedia, error)
	ImageAllowed(ctx context.Context, creds Credentials, conversationID string, conversationType ConversationType) (bool, error)
	ProbeInbox(ctx context.Context, creds Credentials) error
	Participants(ctx context.Context, creds Credentials, conversationID string) ([]Participant, error)
}

type RemoteVideo struct {
	ItemID       string
	MediaType    MediaType
	Caption      string
	ShareURL     string
	EmbedURL     string
	ThumbnailURL string
	IsAd         bool
	Likes        int64
	Comments     int64
	Shares       int64
	Views        int64
	PostedAt     *time.Time
}

type VideoPage struct {
	Videos  []RemoteVideo
	Cursor  int64
	HasMore bool
}

type VideoService interface {
	ListVideos(ctx context.Context, creds Credentials, cursor int64, limit int) (VideoPage, error)
	GetVideos(ctx context.Context, creds Credentials, itemIDs []string) ([]RemoteVideo, error)
}

type RemoteComment struct {
	CommentID        string
	VideoID          string
	ParentCommentID  string
	UniqueIdentifier string
	Username         string
	DisplayName      string
	AvatarURL        string
	Text             string
	ImageURL         string
	Visibility       CommentVisibility
	Owner            bool
	Liked            bool
	Likes            int
	Replies          int
	CreatedAt        *time.Time
}

type CommentQuery struct {
	VideoID        string
	Cursor         int64
	Limit          int
	IncludeHidden  bool
	IncludeReplies bool
}

type CommentPage struct {
	Comments []RemoteComment
	Cursor   int64
	HasMore  bool
}

type CommentService interface {
	ListComments(ctx context.Context, creds Credentials, q CommentQuery) (CommentPage, error)
	LookupComments(ctx context.Context, creds Credentials, videoID string, commentIDs []string) ([]RemoteComment, error)
	Reply(ctx context.Context, creds Credentials, videoID, commentID, text string) (string, error)
	SetHidden(ctx context.Context, creds Credentials, videoID, commentID string, hidden bool) error
	SetLiked(ctx context.Context, creds Credentials, commentID string, liked bool) error
}

type WebhookKind string

const (
	WebhookDirectMessage WebhookKind = "DIRECT_MESSAGE"
	WebhookComment       WebhookKind = "COMMENT"
)

func WebhookKinds() []WebhookKind { return []WebhookKind{WebhookDirectMessage, WebhookComment} }

type WebhookSubscription struct {
	Kind        WebhookKind
	CallbackURL string
	ItemFilter  []string
}

type WebhookConfigService interface {
	Subscription(ctx context.Context, kind WebhookKind) (*WebhookSubscription, error)
	Subscribe(ctx context.Context, kind WebhookKind, callbackURL string) error
}

type ErrorClassifier interface {
	IsAuth(err error) bool
	IsPermission(err error) bool
	IsRateLimited(err error) bool
	IsBudgetBlocked(err error) bool
	IsNotFound(err error) bool
	IsInvalidRequest(err error) bool
	IsEligibility(err error) bool
	IsRetryable(err error) bool
	IsAmbiguous(err error) bool
	IsDefiniteRefusal(err error) bool
}
