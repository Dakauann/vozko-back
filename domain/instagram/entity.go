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

const (
	ScopeBasic          = "instagram_business_basic"
	ScopeManageMessages = "instagram_business_manage_messages"
	ScopeManageComments = "instagram_business_manage_comments"
	ScopeContentPublish = "instagram_business_content_publish"
)

const MessagingWindow = 24 * time.Hour

const ExtendedMessagingWindow = 7 * 24 * time.Hour

const MaxTextBytes = 1000

const PrivateReplyWindow = 7 * 24 * time.Hour

const (
	MaxQuickReplies           = 13
	MaxQuickReplyTitleRunes   = 20
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
		return next == StatusConnected || next == StatusRevoked
	case StatusSuspended:
		return next == StatusConnected || next == StatusRevoked
	case StatusRevoked:
		return next == StatusConnected
	}
	return false
}

type Account struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspaceId"`
	DepartmentID *string `json:"departmentId,omitempty"`

	IGUserID          string `json:"igUserId"`
	Username          string `json:"username"`
	Name              string `json:"name,omitempty"`
	ProfilePictureURL string `json:"profilePictureUrl,omitempty"`
	AccountType       string `json:"accountType,omitempty"`
	FollowersCount    int    `json:"followersCount"`
	FollowsCount      int    `json:"followsCount"`
	MediaCount        int    `json:"mediaCount"`

	AccessToken      string     `json:"-"`
	TokenExpiresAt   *time.Time `json:"tokenExpiresAt,omitempty"`
	TokenRefreshedAt *time.Time `json:"tokenRefreshedAt,omitempty"`
	GrantedScopes    []string   `json:"grantedScopes"`

	AgentID              *string `json:"agentId,omitempty"`
	WorkflowID           *string `json:"workflowId,omitempty"`
	PipelineID           *string `json:"pipelineId,omitempty"`
	EnableAgentResponses bool    `json:"enableAgentResponses"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	EnableAnalysis       bool    `json:"enableAnalysis"`
	EnableAutoStaging    bool    `json:"enableAutoStaging"`
	EnableAutoMemory     bool    `json:"enableAutoMemory"`

	Status       Status `json:"status"`
	StatusReason string `json:"statusReason,omitempty"`

	WebhookSubscribedAt *time.Time `json:"webhookSubscribedAt,omitempty"`
	MessagingHealthy    bool       `json:"messagingHealthy"`
	MessagingCheckedAt  *time.Time `json:"messagingCheckedAt,omitempty"`

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

func (a *Account) HasScope(scope string) bool {
	for _, s := range a.GrantedScopes {
		if s == scope {
			return true
		}
	}
	return false
}

func (a *Account) CanReceiveMessages() bool {
	return a.Status == StatusConnected && a.HasScope(ScopeManageMessages)
}

func (a *Account) CanManageComments() bool {
	return a.Status == StatusConnected && a.HasScope(ScopeManageComments)
}

func (a *Account) CanPublishContent() bool {
	return a.Status == StatusConnected && a.HasScope(ScopeContentPublish)
}

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

	LeadID *string `json:"leadId,omitempty"`

	Blocked   bool      `json:"blocked"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c *Contact) DisplayName() string {
	if n := strings.TrimSpace(c.Name); n != "" {
		return n
	}
	if u := strings.TrimSpace(c.Username); u != "" {
		return "@" + u
	}
	return c.IGSID
}

func (c *Contact) ProfileIsStale(now time.Time, ttl time.Duration) bool {
	if c.ProfileFetchedAt == nil {
		return true
	}
	return now.Sub(*c.ProfileFetchedAt) > ttl
}

type Conversation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	IGAccountID string `json:"igAccountId"`
	ContactID   string `json:"contactId"`

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

func (c *Conversation) WindowOpen(now time.Time) (bool, *time.Time) {
	if c.LastCustomerMessageAt == nil {
		return false, nil
	}
	expires := c.LastCustomerMessageAt.Add(MessagingWindow)
	return now.Before(expires), &expires
}

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

func (m *Media) IsReel() bool { return m.MediaProductType == MediaProductReels }

func (m *Media) IsCarousel() bool { return m.MediaType == MediaTypeCarousel }

type Comment struct {
	ID                string  `json:"id"`
	WorkspaceID       string  `json:"workspaceId"`
	IGAccountID       string  `json:"igAccountId"`
	IGCommentID       string  `json:"igCommentId"`
	IGMediaID         string  `json:"igMediaId"`
	ParentIGCommentID *string `json:"parentIgCommentId,omitempty"`

	FromIGSID    string `json:"fromIgsid,omitempty"`
	FromUsername string `json:"fromUsername,omitempty"`
	Text         string `json:"text"`
	LikeCount    int    `json:"likeCount"`
	Hidden       bool   `json:"hidden"`
	IsOurs       bool   `json:"isOurs"`

	Timestamp *time.Time `json:"timestamp,omitempty"`

	Replies []*Comment `json:"replies,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c *Comment) CanDelete() bool { return c.IsOurs }

type PrivateReplyStatus string

const (
	PrivateReplyAttempted PrivateReplyStatus = "ATTEMPTED"
	PrivateReplySent      PrivateReplyStatus = "SENT"
	PrivateReplyFailed    PrivateReplyStatus = "FAILED"
)

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

func (p *PrivateReply) Consumed() bool {
	return p.Status == PrivateReplySent || p.Status == PrivateReplyAttempted
}
