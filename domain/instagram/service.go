package instagram

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ---------------------------------------------------------------- OAuth

// TokenGrant is the result of a code exchange or a refresh.
type TokenGrant struct {
	AccessToken string
	ExpiresIn   time.Duration
	// UserID is only present on the initial code exchange. It is the Instagram
	// professional account ID.
	UserID string
	// Permissions is the list the user actually granted.
	Permissions []string
}

// Profile is the authenticated account's own profile (GET /me).
type Profile struct {
	// IGUserID comes from the `user_id` field, NOT `id`. `id` is app-scoped and
	// is not usable in endpoint paths.
	IGUserID          string
	Username          string
	Name              string
	AccountType       string
	ProfilePictureURL string
	FollowersCount    int
	FollowsCount      int
	MediaCount        int
}

// OAuthService wraps Business Login for Instagram. Note that the flow spans
// three different hosts (www.instagram.com → api.instagram.com →
// graph.instagram.com), so implementations must not share one base URL.
type OAuthService interface {
	// BuildAuthorizeURL returns the URL to redirect the user to. state must be
	// opaque and verifiable by the caller.
	BuildAuthorizeURL(state string) string
	// ExchangeCode swaps an authorization code for a short-lived token. The
	// upstream response is array-wrapped ({"data":[{...}]}) unlike every other
	// token response in the flow.
	ExchangeCode(ctx context.Context, code string) (*TokenGrant, error)
	// ExchangeForLongLived upgrades a short-lived token to a 60-day token.
	ExchangeForLongLived(ctx context.Context, shortLivedToken string) (*TokenGrant, error)
	// RefreshToken extends a long-lived token. Rejected if the token is younger
	// than 24 hours.
	RefreshToken(ctx context.Context, longLivedToken string) (*TokenGrant, error)
	// GetProfile reads the authenticated account's own profile.
	GetProfile(ctx context.Context, token string) (*Profile, error)
}

// ---------------------------------------------------------------- Webhooks

// SubscriptionService manages per-account webhook subscriptions.
type SubscriptionService interface {
	// Subscribe enables the given webhook fields for an account.
	Subscribe(ctx context.Context, igUserID, token string, fields []string) error
	Unsubscribe(ctx context.Context, igUserID, token string) error
}

// SubscribedFields is the set we register per account.
//
// This list is taken from the API's own error response, not from the docs. Meta
// validates subscribed_fields ATOMICALLY: one invalid entry rejects the entire
// call with code 100, so nothing gets subscribed and no webhook ever arrives. That
// is exactly what happened with "message_echoes", which the documentation lists as
// a subscribable field but the API refuses:
//
//	Param subscribed_fields[1] must be one of {agent_messages, messages,
//	messaging_postbacks, messaging_seen, messaging_handover, messaging_referral,
//	messaging_optins, message_reactions, message_edit, standby, comments,
//	live_comments, mentions, story_insights, ...} - got "message_echoes"
//
// Two further doc corrections from that same response: `mentions` and
// `story_insights` ARE accepted on this login type, contrary to the docs marking
// them unavailable.
//
// Echoes therefore ride on `messages` (flagged with is_echo), which is what the
// webhook decoder already assumes.
func SubscribedFields() []string {
	return []string{
		// Messaging. Echoes arrive on `messages` with is_echo set.
		"messages",
		"message_reactions",
		"message_edit",
		"messaging_seen",
		"messaging_postbacks",
		"messaging_referral",
		"messaging_optins",
		"messaging_handover",
		"standby",

		// Comments and mentions.
		"comments",
		"live_comments",
		"mentions",
	}
}

// validSubscribedFields is the set the API accepts, as reported by its own
// validation error. Used to fail fast with a precise message instead of letting one
// bad entry silently void the whole subscription.
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

// InvalidSubscribedFields returns any entries the API would reject.
//
// Because validation is atomic upstream, a single unknown field means zero webhooks,
// a failure mode with no symptom other than silence. Checking locally turns that
// into an explicit error naming the offending field.
func InvalidSubscribedFields(fields []string) []string {
	var bad []string
	for _, f := range fields {
		if _, ok := validSubscribedFields[f]; !ok {
			bad = append(bad, f)
		}
	}
	return bad
}

// ---------------------------------------------------------------- Messaging

type SendTextInput struct {
	RecipientIGSID string
	Text           string
	// ReplyToMID makes this an inline reply. It is a TOP-LEVEL field in the
	// request body, not nested inside message.
	ReplyToMID string

	// QuickReplies turns the message into a single-choice prompt. They ride on
	// the same /messages call as the text, Instagram has no separate endpoint,
	// and come back as message.quick_reply.payload on the contact's tap.
	QuickReplies []QuickReplyOption
}

// QuickReplyOption is one tappable choice under a message.
//
// Payload is the contract the workflow branches on; Title is a label the author
// may reword at any time. Instagram truncates Title at 20 characters silently.
type QuickReplyOption struct {
	Title   string
	Payload string
}

type SendMediaInput struct {
	RecipientIGSID string
	// Kind is image | video | audio | file.
	Kind string
	// URL must be publicly reachable; Instagram fetches it server-side.
	URL        string
	ReplyToMID string
}

type SendResult struct {
	RecipientID string
	// MessageID is empty for reaction sends, which return only recipient_id.
	MessageID string
}

// ContactProfileResult is a DM participant's public profile.
type ContactProfileResult struct {
	Username             string
	Name                 string
	ProfilePictureURL    string
	IsVerifiedUser       bool
	FollowerCount        int
	IsUserFollowBusiness bool
	IsBusinessFollowUser bool
}

// MessagingService is the Send API surface. Note that three distinct request
// bodies hit the same /messages endpoint (message, sender_action+payload for
// reactions, and bare sender_action for typing/seen), so they are separate
// methods rather than one struct with optional fields.
type MessagingService interface {
	SendText(ctx context.Context, igUserID, token string, in SendTextInput) (*SendResult, error)
	SendMedia(ctx context.Context, igUserID, token string, in SendMediaInput) (*SendResult, error)

	SendReaction(ctx context.Context, igUserID, token, recipientIGSID, targetMID, reaction string) error
	RemoveReaction(ctx context.Context, igUserID, token, recipientIGSID, targetMID string) error

	SendTyping(ctx context.Context, igUserID, token, recipientIGSID string, on bool) error
	MarkSeen(ctx context.Context, igUserID, token, recipientIGSID string) error

	// SendPrivateReply DMs the author of a public comment. The path takes OUR
	// business account id and the comment is addressed via
	// recipient.comment_id. Exactly one is permitted per comment, ever.
	SendPrivateReply(ctx context.Context, igUserID, token, igCommentID, text string) (*SendResult, error)

	// GetContactProfile reads a DM participant's public profile.
	GetContactProfile(ctx context.Context, token, igsid string) (*ContactProfileResult, error)

	// GetConversations is used only as a health probe and for gap repair. It is
	// rate-limited to 2 calls/sec per account, 50× tighter than sending.
	GetConversations(ctx context.Context, igUserID, token string, limit int) error
}

// ---------------------------------------------------------------- Media

// Page carries one page of a cursor-paginated Graph edge.
//
// Cursors are passed through to the client opaquely and never persisted: Meta
// documents that they "can become invalid quickly". A short page does NOT mean
// the end, privacy filtering shrinks pages, so HasNext is derived from the
// presence of paging.next and nothing else.
type Page[T any] struct {
	Items      []T
	NextCursor string
	PrevCursor string
	HasNext    bool
}

// MediaFields is the explicit field list for a post. The edge returns bare
// {id} objects unless fields are requested.
func MediaFields() []string {
	return []string{
		"id", "caption", "media_type", "media_product_type", "media_url",
		"permalink", "thumbnail_url", "timestamp", "username", "like_count",
		"comments_count", "is_comment_enabled", "shortcode",
	}
}

// RemoteMedia is a post as returned by Graph, including the ephemeral CDN URLs
// that must not be persisted.
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

	// MediaURL and ThumbnailURL are short-lived signed CDN links. Serve them
	// through the proxy, never store them.
	MediaURL     string
	ThumbnailURL string

	// Children is populated for carousels when expansion was requested.
	Children []*RemoteMedia
}

type CreateMediaInput struct {
	// ImageURL or VideoURL, publicly reachable. JPEG only for images.
	ImageURL string
	VideoURL string
	Caption  string
	// MediaType is REELS or STORIES for those product types; empty for a plain
	// feed image.
	MediaType string
}

// ContainerStatus reflects Instagram's asynchronous container processing.
type ContainerStatus struct {
	ID         string
	StatusCode string // EXPIRED | ERROR | FINISHED | IN_PROGRESS | PUBLISHED
	Status     string
}

func (c ContainerStatus) Ready() bool  { return c.StatusCode == "FINISHED" }
func (c ContainerStatus) Failed() bool { return c.StatusCode == "ERROR" || c.StatusCode == "EXPIRED" }

// MediaService is the posts surface.
type MediaService interface {
	ListMedia(ctx context.Context, igUserID, token string, limit int, after string) (*Page[*RemoteMedia], error)
	GetMedia(ctx context.Context, token, igMediaID string, withChildren bool) (*RemoteMedia, error)

	// CreateContainer starts a publish. Container processing is asynchronous.
	CreateContainer(ctx context.Context, igUserID, token string, in CreateMediaInput) (containerID string, err error)
	GetContainerStatus(ctx context.Context, token, containerID string) (*ContainerStatus, error)
	PublishContainer(ctx context.Context, igUserID, token, containerID string) (igMediaID string, err error)

	// SetCommentEnabled is the ONLY supported update on a published post.
	// Captions are immutable via the API.
	SetCommentEnabled(ctx context.Context, token, igMediaID string, enabled bool) error

	// FetchMediaBytes streams a CDN asset so we can proxy it without persisting
	// the expiring URL.
	FetchMediaBytes(ctx context.Context, url string) (data []byte, contentType string, err error)
}

// ---------------------------------------------------------------- Comments

// CommentFields is the explicit field list for a comment. `replies` is expanded
// so a threaded view costs one round trip instead of N+1.
func CommentFields() []string {
	return []string{
		"id", "text", "timestamp", "username", "from", "like_count",
		"hidden", "parent_id", "user",
	}
}

// RemoteComment is a comment as returned by Graph.
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
	// IsOurs is true when the `user` field was present, which Instagram does
	// only for comments our own app user authored.
	IsOurs  bool
	Replies []*RemoteComment
}

// CommentService is the comment moderation surface.
type CommentService interface {
	ListComments(ctx context.Context, token, igMediaID string, limit int, after string) (*Page[*RemoteComment], error)
	GetComment(ctx context.Context, token, igCommentID string) (*RemoteComment, error)
	ListReplies(ctx context.Context, token, igCommentID string, limit int, after string) (*Page[*RemoteComment], error)

	// ReplyToComment posts a threaded reply.
	ReplyToComment(ctx context.Context, token, igCommentID, message string) (newIGCommentID string, err error)
	// CreateComment posts a top-level comment on a post.
	CreateComment(ctx context.Context, token, igMediaID, message string) (newIGCommentID string, err error)

	// SetHidden hides or unhides a comment. This requires the media owner's
	// token and is the moderation action for someone else's comment.
	SetHidden(ctx context.Context, token, igCommentID string, hidden bool) error
	// Delete removes a comment. Requires the COMMENT CREATOR's token, so it only
	// works for comments we authored.
	Delete(ctx context.Context, token, igCommentID string) error
}

// RequiredScopes is the permission set the Instagram channel needs.
//
// This is deliberately code, not configuration. The scopes are a hard contract
// with the implementation, the messaging code cannot work without
// instagram_business_manage_messages, comment moderation cannot work without
// instagram_business_manage_comments, and they are also exactly what was
// submitted for App Review. Making them env-tunable would let a deployment typo
// silently disable a feature at consent time, surfacing much later as a confusing
// runtime permission error.
//
// Note these are the long forms: the short names (business_basic,
// business_manage_messages, ...) were deprecated on 2025-01-27.
func RequiredScopes() []string {
	return []string{
		ScopeBasic,
		ScopeManageMessages,
		ScopeManageComments,
		ScopeContentPublish,
	}
}

// ---------------------------------------------------------------- OAuth paths

// The OAuth paths are owned by the code, not by configuration.
//
// In OAuth the redirect URI is registered at the authorization server, which then
// exact-matches whatever the client sends, so the value necessarily exists in two
// places (here, and Meta's App Dashboard allowlist). That duplication IS the
// security boundary and cannot be removed: if Meta accepted any redirect_uri we
// sent, an attacker able to reach our start endpoint could have authorization codes
// delivered to a host they control.
//
// What CAN be removed is drift on our own side. The PATH is a constant shared by the
// router and by config validation, so a deployment only chooses the host. Before
// this was a constant, the env var and the registered route disagreed and produced
// an "Invalid redirect_uri" that looked like a Meta problem.
const (
	OAuthStartPath    = "/oauth/instagram/start"
	OAuthCallbackPath = "/oauth/instagram/callback"
)

// ValidateRedirectURI checks that a configured redirect URI is one this build can
// actually serve.
//
// It deliberately does NOT check the host: only the deployment knows its public
// hostname, and Meta is the authority on which hosts are allowlisted. It checks the
// parts we own, scheme and path, so a typo fails at boot with a precise message
// instead of surfacing as an opaque Instagram error mid-onboarding.
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

	// Instagram requires https, except on localhost for local development.
	isLocal := strings.HasPrefix(parsed.Hostname(), "localhost") || parsed.Hostname() == "127.0.0.1"
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLocal) {
		return fmt.Errorf("instagram: redirect URI %q must use https (http is only allowed on localhost)", raw)
	}

	// A trailing slash is tolerated: Meta's dashboard is documented to sometimes
	// append one to a saved URI, and the router accepts both spellings.
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

// RedirectURIFor builds the canonical redirect URI from a public API base URL, so a
// deployment configures just the host and never restates the path.
func RedirectURIFor(apiBaseURL string) string {
	return strings.TrimRight(strings.TrimSpace(apiBaseURL), "/") + OAuthCallbackPath
}
