package instagram

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrAccountNotFound       = errors.New("instagram account not found")
	ErrAccountAlreadyLinked  = errors.New("instagram account is already connected")
	ErrIGUserIDRequired      = errors.New("instagram user id is required")
	ErrWorkspaceIDRequired   = errors.New("workspace id is required")
	ErrAccessTokenRequired   = errors.New("instagram access token is required")
	ErrMissingMessagingScope = errors.New("instagram account did not grant the messaging permission")
	ErrContactNotFound       = errors.New("instagram contact not found")
	ErrConversationNotFound  = errors.New("instagram conversation not found")
	ErrMediaNotFound         = errors.New("instagram media not found")
	ErrCommentNotFound       = errors.New("instagram comment not found")
	ErrInvalidStatus         = errors.New("invalid instagram account status")
	ErrStatusTransition      = errors.New("invalid instagram account status transition")
	ErrWindowClosed          = errors.New("instagram 24h messaging window is closed")
	ErrTextTooLong           = errors.New("instagram message text exceeds 1000 bytes")
	ErrPrivateReplyUsed      = errors.New("a private reply was already sent for this comment")
	ErrPrivateReplyExpired   = errors.New("private replies must be sent within 7 days of the comment")
	ErrCaptionImmutable      = errors.New("instagram does not support editing a published caption")
	ErrDeleteNotSupported    = errors.New("deleting media requires Instagram API with Facebook Login")
)

// Scope names for the Instagram Login flow. The short forms (business_basic
// etc.) were deprecated 2025-01-27, only these are valid.
const (
	ScopeBasic          = "instagram_business_basic"
	ScopeManageMessages = "instagram_business_manage_messages"
	ScopeManageComments = "instagram_business_manage_comments"
	ScopeContentPublish = "instagram_business_content_publish"
)

// MessagingWindow is how long we may reply after the contact's last message.
const MessagingWindow = 24 * time.Hour

// ExtendedMessagingWindow is the human_agent window. Reaching it requires an
// approved escalation; see the plan's Phase-1 spike before enabling.
const ExtendedMessagingWindow = 7 * 24 * time.Hour

// MaxTextBytes is Instagram's documented limit: "1000 bytes or less".
const MaxTextBytes = 1000

// PrivateReplyWindow, a private reply must be sent within 7 days of the comment.
const PrivateReplyWindow = 7 * 24 * time.Hour

// Quick-reply limits. "A maximum of 13 quick replies are supported" and "Each
// quick reply allows up to 20 characters before being truncated", Instagram
// truncates the label itself rather than rejecting the send, so an over-long
// label is a silent product defect, not an error we would ever see.
const (
	MaxQuickReplies         = 13
	MaxQuickReplyTitleRunes = 20
	// Instagram's own reference states no payload bound; 1000 is the Messenger
	// Platform limit this surface inherits.
	MaxQuickReplyPayloadBytes = 1000
)

type Status string

const (
	StatusPending      Status = "PENDING"
	StatusConnected    Status = "CONNECTED"
	StatusTokenExpired Status = "TOKEN_EXPIRED"
	StatusRevoked      Status = "REVOKED"
	StatusSuspended    Status = "SUSPENDED"
)

func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusConnected, StatusTokenExpired, StatusRevoked, StatusSuspended:
		return true
	}
	return false
}

// CanTransitionTo guards the lifecycle. The WhatsApp phone entity has ten states
// and no guard at all, every mutation is a bare field assignment. We add the
// guard here from day one so an invalid transition is a domain error, not a
// silent corruption.
func (s Status) CanTransitionTo(next Status) bool {
	if !next.Valid() {
		return false
	}
	if s == next {
		return true
	}
	switch s {
	case StatusPending:
		return next == StatusConnected || next == StatusRevoked
	case StatusConnected:
		return next == StatusTokenExpired || next == StatusRevoked || next == StatusSuspended
	case StatusTokenExpired:
		// Reconnecting re-runs OAuth and lands back on CONNECTED.
		return next == StatusConnected || next == StatusRevoked
	case StatusSuspended:
		return next == StatusConnected || next == StatusRevoked
	case StatusRevoked:
		// Terminal until the row is restored by a fresh onboarding.
		return next == StatusConnected
	}
	return false
}

// Account is a connected Instagram professional account. It doubles as the
// config carrier for its conversations, the role whatsapp_campaigns plays for
// WhatsApp, which is why the automation fields live here. Instagram has no
// campaign concept: outbound-first messaging is impossible, so there is nothing
// to blast and no template to carry.
type Account struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspaceId"`
	DepartmentID *string `json:"departmentId,omitempty"`

	// IGUserID is the Instagram professional account ID, the `user_id` field
	// from GET /me, NOT the app-scoped `id`. It is what goes in endpoint paths.
	IGUserID          string `json:"igUserId"`
	Username          string `json:"username"`
	Name              string `json:"name,omitempty"`
	ProfilePictureURL string `json:"profilePictureUrl,omitempty"`
	AccountType       string `json:"accountType,omitempty"`
	FollowersCount    int    `json:"followersCount"`
	FollowsCount      int    `json:"followsCount"`
	MediaCount        int    `json:"mediaCount"`

	// AccessToken is never serialized. Encrypted at rest.
	AccessToken      string     `json:"-"`
	TokenExpiresAt   *time.Time `json:"tokenExpiresAt,omitempty"`
	TokenRefreshedAt *time.Time `json:"tokenRefreshedAt,omitempty"`
	// GrantedScopes is the permission list the user ACTUALLY granted. Users can
	// decline individual scopes, so we must not assume we got what we asked for.
	GrantedScopes []string `json:"grantedScopes"`

	AgentID              *string `json:"agentId,omitempty"`
	WorkflowID           *string `json:"workflowId,omitempty"`
	PipelineID           *string `json:"pipelineId,omitempty"`
	EnableAgentResponses bool    `json:"enableAgentResponses"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	EnableAnalysis       bool    `json:"enableAnalysis"`
	EnableAutoStaging    bool    `json:"enableAutoStaging"`

	Status       Status `json:"status"`
	StatusReason string `json:"statusReason,omitempty"`

	WebhookSubscribedAt *time.Time `json:"webhookSubscribedAt,omitempty"`
	// MessagingHealthy tracks the "Allow Access to Messages" toggle in the
	// Instagram app. There is no API for that flag, so this is a probe result:
	// when it is off, DMs and messaging webhooks fail silently despite a fully
	// successful OAuth.
	MessagingHealthy   bool       `json:"messagingHealthy"`
	MessagingCheckedAt *time.Time `json:"messagingCheckedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (a *Account) Normalize() {
	a.IGUserID = strings.TrimSpace(a.IGUserID)
	a.Username = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(a.Username), "@"))
	a.Name = strings.TrimSpace(a.Name)
	a.AccountType = strings.ToUpper(strings.TrimSpace(a.AccountType))
	if a.Status == "" {
		a.Status = StatusPending
	}
	cleaned := make([]string, 0, len(a.GrantedScopes))
	seen := make(map[string]struct{}, len(a.GrantedScopes))
	for _, s := range a.GrantedScopes {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		cleaned = append(cleaned, s)
	}
	a.GrantedScopes = cleaned
}

func (a *Account) Validate() error {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return ErrWorkspaceIDRequired
	}
	if strings.TrimSpace(a.IGUserID) == "" {
		return ErrIGUserIDRequired
	}
	if !a.Status.Valid() {
		return ErrInvalidStatus
	}
	return nil
}

// HasScope reports whether a permission was actually granted.
func (a *Account) HasScope(scope string) bool {
	for _, s := range a.GrantedScopes {
		if s == scope {
			return true
		}
	}
	return false
}

// CanReceiveMessages reports whether this account is usable for DMs at all.
func (a *Account) CanReceiveMessages() bool {
	return a.Status == StatusConnected && a.HasScope(ScopeManageMessages)
}

// CanManageComments reports whether comment moderation and private replies are
// available. Private replies are gated by the COMMENTS scope, not messaging.
func (a *Account) CanManageComments() bool {
	return a.Status == StatusConnected && a.HasScope(ScopeManageComments)
}

func (a *Account) CanPublishContent() bool {
	return a.Status == StatusConnected && a.HasScope(ScopeContentPublish)
}

// TokenNeedsRefresh reports whether the long-lived token should be refreshed.
// Instagram refuses a refresh on a token younger than 24 hours, and a token
// unused for 60 days dies permanently, so we refresh well ahead of expiry.
func (a *Account) TokenNeedsRefresh(now time.Time, lead time.Duration) bool {
	if a.Status != StatusConnected {
		return false
	}
	if a.TokenExpiresAt == nil {
		return false
	}
	if now.Before(a.TokenExpiresAt.Add(-lead)) {
		return false
	}
	if a.TokenRefreshedAt != nil && now.Sub(*a.TokenRefreshedAt) < 24*time.Hour {
		return false
	}
	return true
}

// Contact is a person who messaged one of our Instagram accounts.
//
// The identity is (account, IGSID), never IGSID alone. An Instagram-scoped ID
// is scoped to the (app, professional account) pair, so the same human has a
// DIFFERENT IGSID on each connected account.
type Contact struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	IGAccountID string `json:"igAccountId"`
	IGSID       string `json:"igsid"`

	Username             string     `json:"username,omitempty"`
	Name                 string     `json:"name,omitempty"`
	ProfilePictureURL    string     `json:"profilePictureUrl,omitempty"`
	IsVerifiedUser       bool       `json:"isVerifiedUser"`
	FollowerCount        int        `json:"followerCount"`
	IsUserFollowBusiness bool       `json:"isUserFollowBusiness"`
	IsBusinessFollowUser bool       `json:"isBusinessFollowUser"`
	ProfileFetchedAt     *time.Time `json:"profileFetchedAt,omitempty"`

	// LeadID optionally bridges this contact to a WhatsApp lead so the same
	// human can be recognised across channels. Nullable and unused by the base
	// implementation, it exists so cross-channel identity can be added later
	// without a migration.
	LeadID *string `json:"leadId,omitempty"`

	Blocked   bool      `json:"blocked"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// DisplayName prefers the real name, falling back to the handle.
func (c *Contact) DisplayName() string {
	if n := strings.TrimSpace(c.Name); n != "" {
		return n
	}
	if u := strings.TrimSpace(c.Username); u != "" {
		return "@" + u
	}
	return c.IGSID
}

// ProfileIsStale reports whether we should re-fetch the contact profile. Reads
// cost against a budget that scales with the account's audience activity
// (4800 × impressions per 24h), so enrichment is lazy.
func (c *Contact) ProfileIsStale(now time.Time, ttl time.Duration) bool {
	if c.ProfileFetchedAt == nil {
		return true
	}
	return now.Sub(*c.ProfileFetchedAt) > ttl
}

// Conversation is the ENTRY, the thing the CRM treats as a conversation. It
// carries the same state contract as whatsapp_campaign_entries so labels,
// stages, opportunities and inbox assignment (all keyed on entry_id+entry_type)
// work without any change to those subsystems.
type Conversation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	IGAccountID string `json:"igAccountId"`
	ContactID   string `json:"contactId"`

	// IGConversationID is Meta's conversation id. Nullable because ingest is
	// webhook-first: we learn the thread from a message long before we would
	// ever call the Conversations API.
	IGConversationID *string `json:"igConversationId,omitempty"`

	ConversationStatus string     `json:"conversationStatus,omitempty"`
	CloseSource        string     `json:"closeSource,omitempty"`
	CloseReason        string     `json:"closeReason,omitempty"`
	ClosedAt           *time.Time `json:"closedAt,omitempty"`
	AutomationEnabled  *bool      `json:"automationEnabled,omitempty"`

	LastMessageAt         *time.Time `json:"lastMessageAt,omitempty"`
	LastCustomerMessageAt *time.Time `json:"lastCustomerMessageAt,omitempty"`
	LastAgentMessageAt    *time.Time `json:"lastAgentMessageAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// WindowOpen reports whether we may send right now, and when the window closes.
// The 24h clock is a sliding deadline anchored on the contact's last inbound
// message, so it resets every time they write to us.
func (c *Conversation) WindowOpen(now time.Time) (bool, *time.Time) {
	if c.LastCustomerMessageAt == nil {
		return false, nil
	}
	expires := c.LastCustomerMessageAt.Add(MessagingWindow)
	return now.Before(expires), &expires
}

// MediaProductType distinguishes reels and stories. There is no
// media_type=REELS, media_type is only IMAGE/VIDEO/CAROUSEL_ALBUM, so all
// per-type logic must branch on THIS field or every reel reads as a plain video.
type MediaProductType string

const (
	MediaProductFeed  MediaProductType = "FEED"
	MediaProductReels MediaProductType = "REELS"
	MediaProductStory MediaProductType = "STORY"
	MediaProductAd    MediaProductType = "AD"
)

type MediaType string

const (
	MediaTypeImage    MediaType = "IMAGE"
	MediaTypeVideo    MediaType = "VIDEO"
	MediaTypeCarousel MediaType = "CAROUSEL_ALBUM"
)

// Media is a post. Only durable identifiers are persisted: media_url and
// thumbnail_url are short-lived signed CDN links that stop resolving, so they
// are fetched on demand and proxied rather than stored.
type Media struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	IGAccountID string `json:"igAccountId"`
	IGMediaID   string `json:"igMediaId"`

	MediaType        MediaType        `json:"mediaType"`
	MediaProductType MediaProductType `json:"mediaProductType"`
	Caption          string           `json:"caption,omitempty"`
	Permalink        string           `json:"permalink,omitempty"`
	Shortcode        string           `json:"shortcode,omitempty"`
	Timestamp        *time.Time       `json:"timestamp,omitempty"`

	LikeCount        int   `json:"likeCount"`
	CommentsCount    int   `json:"commentsCount"`
	IsCommentEnabled *bool `json:"isCommentEnabled,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// IsReel reports whether this post is a reel, which is a VIDEO with a REELS
// product type rather than a distinct media_type.
func (m *Media) IsReel() bool { return m.MediaProductType == MediaProductReels }

func (m *Media) IsCarousel() bool { return m.MediaType == MediaTypeCarousel }

// Comment is a comment or a reply on one of our posts.
type Comment struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	IGAccountID string `json:"igAccountId"`
	IGCommentID string `json:"igCommentId"`
	IGMediaID   string `json:"igMediaId"`
	// ParentIGCommentID set means this is a reply to another comment.
	ParentIGCommentID *string `json:"parentIgCommentId,omitempty"`

	FromIGSID    string `json:"fromIgsid,omitempty"`
	FromUsername string `json:"fromUsername,omitempty"`
	Text         string `json:"text"`
	LikeCount    int    `json:"likeCount"`
	Hidden       bool   `json:"hidden"`
	// IsOurs is derived from the presence of the `user` field, which Instagram
	// populates only when our own app user authored the comment. It decides
	// whether deletion is even possible: DELETE needs the comment creator's
	// token, so we can only delete our own replies.
	IsOurs bool `json:"isOurs"`

	Timestamp *time.Time `json:"timestamp,omitempty"`

	// Replies is populated when the caller requested reply expansion.
	Replies []*Comment `json:"replies,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CanDelete reports whether we may delete this comment. Hiding is the moderation
// action for anyone else's comment.
func (c *Comment) CanDelete() bool { return c.IsOurs }

// PrivateReplyStatus tracks the one-shot private-reply allowance.
type PrivateReplyStatus string

const (
	PrivateReplyAttempted PrivateReplyStatus = "ATTEMPTED"
	PrivateReplySent      PrivateReplyStatus = "SENT"
	PrivateReplyFailed    PrivateReplyStatus = "FAILED"
)

// PrivateReply records an attempt to DM a commenter. Instagram permits exactly
// one private reply per comment, ever, so the row is written BEFORE the HTTP
// call: a retry after an ambiguous timeout must never burn the allowance.
type PrivateReply struct {
	IGCommentID    string             `json:"igCommentId"`
	IGAccountID    string             `json:"igAccountId"`
	Status         PrivateReplyStatus `json:"status"`
	RecipientIGSID *string            `json:"recipientIgsid,omitempty"`
	IGMessageID    *string            `json:"igMessageId,omitempty"`
	ErrorCode      int                `json:"errorCode,omitempty"`
	ErrorMessage   string             `json:"errorMessage,omitempty"`
	AttemptedAt    time.Time          `json:"attemptedAt"`
	UpdatedAt      time.Time          `json:"updatedAt"`
}

// Consumed reports whether the single allowance is gone. ATTEMPTED counts as
// consumed: we cannot know whether Meta processed the send.
func (p *PrivateReply) Consumed() bool {
	return p.Status == PrivateReplySent || p.Status == PrivateReplyAttempted
}
