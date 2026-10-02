package facebook

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	OAuthStartPath    = "/oauth/facebook/start"
	OAuthCallbackPath = "/oauth/facebook/callback"
)

func ValidateRedirectURI(raw string) error {
	return ValidateRedirectURIFor(raw, OAuthCallbackPath)
}

func ValidateRedirectURIFor(raw, callbackPath string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("facebook: redirect URI is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("facebook: redirect URI %q is not a valid URL: %w", raw, err)
	}
	if parsed.Host == "" {
		return fmt.Errorf("facebook: redirect URI %q must be absolute (scheme + host)", raw)
	}
	isLocal := strings.HasPrefix(parsed.Hostname(), "localhost") || parsed.Hostname() == "127.0.0.1"
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLocal) {
		return fmt.Errorf("facebook: redirect URI %q must use https (http is only allowed on localhost)", raw)
	}
	if strings.TrimSuffix(parsed.Path, "/") != callbackPath {
		return fmt.Errorf("facebook: redirect URI path is %q but this build serves %q; only the host is configurable "+
			"and the full URI must also be listed under Facebook Login for Business > Settings > Valid OAuth Redirect URIs",
			parsed.Path, callbackPath)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("facebook: redirect URI %q must not carry a query string or fragment", raw)
	}
	return nil
}

type TokenGrant struct {
	AccessToken string
	ExpiresAt   *time.Time
}

type TokenDebug struct {
	Valid          bool
	Type           string
	AppScopedUser  string
	Scopes         []string
	GranularScopes map[string][]string
	ExpiresAt      *time.Time
}

func (d *TokenDebug) Kind() TokenKind {
	if strings.EqualFold(d.Type, string(TokenSystemUser)) {
		return TokenSystemUser
	}
	return TokenUser
}

type GrantIdentity struct {
	AppScopedUserID  string
	Name             string
	ClientBusinessID string
}

type RemotePage struct {
	FBPageID       string
	Name           string
	Username       string
	Category       string
	Link           string
	AccessToken    string
	Tasks          []Task
	PictureURL     string
	FollowersCount int
	LinkedIGUserID string
}

type OAuthService interface {
	BuildAuthorizeURL(state string) string
	ExchangeCode(ctx context.Context, code string) (*TokenGrant, error)
	DebugToken(ctx context.Context, token string) (*TokenDebug, error)
	Identify(ctx context.Context, token string, kind TokenKind) (*GrantIdentity, error)
	ListPages(ctx context.Context, token string) ([]*RemotePage, error)
	GetPage(ctx context.Context, pageToken, fbPageID string) (*RemotePage, error)
}

type SubscriptionService interface {
	Subscribe(ctx context.Context, fbPageID, pageToken string, fields []string) ([]string, error)
	ActiveFields(ctx context.Context, fbPageID, pageToken string) ([]string, error)
	Unsubscribe(ctx context.Context, fbPageID, pageToken string) error
}

func SubscribedFields() []string {
	return []string{
		"messages", "message_echoes", "message_deliveries", "message_reads",
		"message_reactions", "message_edits", "messaging_postbacks", "messaging_optins",
		"messaging_referrals", "messaging_handovers", "messaging_policy_enforcement",
		"standby", "feed", "mention", "videos",
	}
}

var validSubscribedFields = map[string]struct{}{
	"feed": {}, "mention": {}, "name": {}, "picture": {}, "category": {}, "description": {},
	"conversations": {}, "standby": {}, "messages": {}, "message_reactions": {},
	"messaging_account_linking": {}, "message_echoes": {}, "message_edits": {},
	"message_deliveries": {}, "messaging_optins": {}, "messaging_optouts": {},
	"messaging_postbacks": {}, "message_reads": {}, "messaging_referrals": {},
	"messaging_handovers": {}, "messaging_policy_enforcement": {}, "response_feedback": {},
	"messaging_feedback": {}, "videos": {}, "live_videos": {}, "ratings": {}, "leadgen": {},
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

type SendTier string

const (
	SendStandard   SendTier = "standard"
	SendHumanAgent SendTier = "human_agent"
)

type Recipient struct {
	PSID      string
	CommentID string
	PostID    string
}

type Option struct {
	Title   string
	Payload string
}

type OutboundMessage struct {
	Recipient Recipient
	Tier      SendTier
	Metadata  string

	Text         string
	ReplyToMID   string
	QuickReplies []Option
	Buttons      []Option

	AttachmentKind string
	AttachmentURL  string
	AttachmentID   string
}

type UploadInput struct {
	Kind     string
	URL      string
	Bytes    []byte
	MIMEType string
	FileName string
}

type SendResult struct {
	RecipientID  string
	MessageID    string
	AttachmentID string
}

type ProfileResult struct {
	Name       string
	FirstName  string
	LastName   string
	PictureURL string
}

type SenderAction string

const (
	ActionTypingOn  SenderAction = "typing_on"
	ActionTypingOff SenderAction = "typing_off"
	ActionMarkSeen  SenderAction = "mark_seen"
)

type MessagingService interface {
	Send(ctx context.Context, fbPageID, pageToken string, msg OutboundMessage) (*SendResult, error)
	SendAction(ctx context.Context, fbPageID, pageToken, psid string, action SenderAction) error
	React(ctx context.Context, fbPageID, pageToken, psid, mid, reaction string) error
	Unreact(ctx context.Context, fbPageID, pageToken, psid, mid string) error
	Upload(ctx context.Context, fbPageID, pageToken string, in UploadInput) (attachmentID string, err error)
	GetProfile(ctx context.Context, pageToken, psid string) (*ProfileResult, error)
	FetchBytes(ctx context.Context, url string) ([]byte, string, error)
}

type RoutingService interface {
	ThreadOwner(ctx context.Context, fbPageID, pageToken, psid string) (string, error)
	TakeControl(ctx context.Context, fbPageID, pageToken, psid, metadata string) error
	ReleaseControl(ctx context.Context, fbPageID, pageToken, psid string) error
}

type Paged[T any] struct {
	Items      []T
	NextCursor string
	HasNext    bool
}

type PostService interface {
	List(ctx context.Context, fbPageID, pageToken string, kind PostListKind, limit int, after string) (*Paged[*RemotePost], error)
	Get(ctx context.Context, pageToken, fbPostID string) (*RemotePost, error)
	CreateFeedPost(ctx context.Context, fbPageID, pageToken string, in FeedPostInput) (string, error)
	UploadPhoto(ctx context.Context, fbPageID, pageToken string, in PhotoInput) (*PhotoResult, error)
	Update(ctx context.Context, pageToken, fbPostID string, in PostUpdate) error
	Delete(ctx context.Context, pageToken, fbObjectID string) error
	FetchBytes(ctx context.Context, url string) ([]byte, string, error)

	ListStories(ctx context.Context, fbPageID, pageToken string) ([]*RemoteStory, error)
	CreateVideo(ctx context.Context, fbPageID, pageToken string, in VideoInput) (string, error)
	StartVideoUpload(ctx context.Context, fbPageID, pageToken string, target VideoTarget) (*VideoSession, error)
	TransferVideo(ctx context.Context, pageToken string, session VideoSession, fileURL string) error
	FinishReel(ctx context.Context, fbPageID, pageToken, videoID string, in ReelFinish) (string, error)
	FinishVideoStory(ctx context.Context, fbPageID, pageToken, videoID string) (string, error)
	CreatePhotoStory(ctx context.Context, fbPageID, pageToken, photoID string) (string, error)
	VideoStatus(ctx context.Context, pageToken, videoID string) (*VideoStatus, error)
}

type ProfileSettingsService interface {
	Get(ctx context.Context, fbPageID, pageToken string) (*MessengerProfile, error)
	Set(ctx context.Context, fbPageID, pageToken string, profile MessengerProfile) error
	Delete(ctx context.Context, fbPageID, pageToken string, fields []string) error
}

type CommentService interface {
	List(ctx context.Context, pageToken, fbObjectID string, filter CommentFilter, limit int, after string) (*Paged[*RemoteComment], error)
	Get(ctx context.Context, pageToken, fbCommentID string) (*RemoteComment, error)
	Create(ctx context.Context, pageToken, fbParentID, message string) (string, error)
	Edit(ctx context.Context, pageToken, fbCommentID, message string) error
	SetHidden(ctx context.Context, pageToken, fbCommentID string, hidden bool) error
	Delete(ctx context.Context, pageToken, fbCommentID string) error
	SetLiked(ctx context.Context, pageToken, fbCommentID string, liked bool) error
}

func (d *TokenDebug) Grants(scope string) bool {
	for _, s := range d.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

func (d *TokenDebug) AdoptListedPages(listed []string) error {
	if !d.Grants(ScopeShowList) {
		return ErrGrantUnverifiable
	}
	if len(d.GranularScopes[ScopeShowList]) > 0 {
		return nil
	}
	if d.GranularScopes == nil {
		d.GranularScopes = map[string][]string{}
	}
	d.GranularScopes[ScopeShowList] = append([]string(nil), listed...)
	return nil
}
