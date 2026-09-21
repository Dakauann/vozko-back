package telegram

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

var (
	ErrAccountNotFound      = errors.New("telegram account not found")
	ErrAccountAlreadyLinked = errors.New("telegram bot is already connected")
	ErrBotTokenRequired     = errors.New("telegram bot token is required")
	ErrBotTokenInvalid      = errors.New("telegram bot token is not valid")
	ErrWorkspaceIDRequired  = errors.New("workspace id is required")
	ErrContactNotFound      = errors.New("telegram contact not found")
	ErrConversationNotFound = errors.New("telegram conversation not found")
	ErrDeepLinkNotFound     = errors.New("telegram deep link not found")
	ErrInvalidStatus        = errors.New("invalid telegram account status")
	ErrStatusTransition     = errors.New("invalid telegram account status transition")
	ErrContactBlocked       = errors.New("the contact has blocked this bot")
	ErrWindowClosed         = errors.New("telegram business 24h messaging window is closed")
	ErrTextTooLong          = errors.New("telegram message text exceeds 4096 characters")
	ErrCannotReply          = errors.New("the business connection does not grant the reply right")
	ErrFileTooLarge         = errors.New("telegram bots cannot download files larger than 20MB")
	ErrInvalidMode          = errors.New("invalid telegram account mode")
)

const MaxTextRunes = 4096

const MaxCaptionRunes = 1024

const MaxDownloadBytes int64 = 20 << 20

const (
	MaxUploadPhotoBytes int64 = 10 << 20
	MaxUploadOtherBytes int64 = 50 << 20
)

const BusinessMessagingWindow = 24 * time.Hour

const (
	EditWindow   = 48 * time.Hour
	DeleteWindow = 48 * time.Hour
)

const MaxCallbackDataBytes = 64

const MaxInlineKeyboardButtons = 100

const InlineKeyboardColumns = 1

const MaxDeepLinkPayload = 64

const (
	PerChatMessagesPerSecond = 1
	PerBotMessagesPerSecond  = 30
)

type Mode string

const (
	ModeBot Mode = "BOT"

	ModeBusiness Mode = "BUSINESS"
)

func (m Mode) Valid() bool { return m == ModeBot || m == ModeBusiness }

type Status string

const (
	StatusPending        Status = "PENDING"
	StatusActive         Status = "ACTIVE"
	StatusTokenInvalid   Status = "TOKEN_INVALID"
	StatusWebhookFailing Status = "WEBHOOK_FAILING"
	StatusRevoked        Status = "REVOKED"
)

func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusActive, StatusTokenInvalid, StatusWebhookFailing, StatusRevoked:
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
		return next == StatusActive || next == StatusRevoked || next == StatusTokenInvalid
	case StatusActive:
		return next == StatusTokenInvalid || next == StatusWebhookFailing || next == StatusRevoked
	case StatusWebhookFailing:
		return next == StatusActive || next == StatusTokenInvalid || next == StatusRevoked
	case StatusTokenInvalid:
		return next == StatusActive || next == StatusRevoked
	case StatusRevoked:
		return next == StatusActive
	}
	return false
}

type BusinessRights struct {
	CanReply              bool `json:"can_reply,omitempty"`
	CanReadMessages       bool `json:"can_read_messages,omitempty"`
	CanDeleteSentMessages bool `json:"can_delete_sent_messages,omitempty"`
	CanDeleteAllMessages  bool `json:"can_delete_all_messages,omitempty"`
	CanEditName           bool `json:"can_edit_name,omitempty"`
	CanEditBio            bool `json:"can_edit_bio,omitempty"`
	CanEditProfilePhoto   bool `json:"can_edit_profile_photo,omitempty"`
	CanEditUsername       bool `json:"can_edit_username,omitempty"`
	CanManageStories      bool `json:"can_manage_stories,omitempty"`
}

type Account struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspaceId"`
	DepartmentID *string `json:"departmentId,omitempty"`

	Mode Mode `json:"mode"`

	BotUserID            int64  `json:"botUserId"`
	BotUsername          string `json:"botUsername"`
	BotName              string `json:"botName,omitempty"`
	CanConnectToBusiness bool   `json:"canConnectToBusiness"`

	BotToken      string `json:"-"`
	WebhookSecret string `json:"-"`

	WebhookSetAt        *time.Time `json:"webhookSetAt,omitempty"`
	WebhookPendingCount int        `json:"webhookPendingCount"`
	WebhookLastError    string     `json:"webhookLastError,omitempty"`
	WebhookLastErrorAt  *time.Time `json:"webhookLastErrorAt,omitempty"`
	WebhookCheckedAt    *time.Time `json:"webhookCheckedAt,omitempty"`

	BusinessConnectionID *string         `json:"businessConnectionId,omitempty"`
	BusinessUserID       *int64          `json:"businessUserId,omitempty"`
	BusinessUsername     string          `json:"businessUsername,omitempty"`
	BusinessRights       *BusinessRights `json:"businessRights,omitempty"`
	BusinessEnabled      bool            `json:"businessEnabled"`

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

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (a *Account) Normalize() {
	a.BotUsername = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(a.BotUsername), "@"))
	a.BotName = strings.TrimSpace(a.BotName)
	a.BusinessUsername = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(a.BusinessUsername), "@"))
	if a.Mode == "" {
		a.Mode = ModeBot
	}
	if a.Status == "" {
		a.Status = StatusPending
	}
}

func (a *Account) Validate() error {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return ErrWorkspaceIDRequired
	}
	if a.BotUserID == 0 {
		return ErrBotTokenInvalid
	}
	if !a.Mode.Valid() {
		return ErrInvalidMode
	}
	if !a.Status.Valid() {
		return ErrInvalidStatus
	}
	return nil
}

func (a *Account) CanSend() bool {
	if a.BotToken == "" {
		return false
	}
	switch a.Status {
	case StatusActive, StatusWebhookFailing:
	default:
		return false
	}
	if a.Mode == ModeBusiness {
		return a.BusinessEnabled && a.Rights().CanReply
	}
	return true
}

func (a *Account) Rights() BusinessRights {
	if a.BusinessRights == nil {
		return BusinessRights{}
	}
	return *a.BusinessRights
}

func (a *Account) DisplayName() string {
	if a.Mode == ModeBusiness && a.BusinessUsername != "" {
		return "@" + a.BusinessUsername
	}
	if a.BotUsername != "" {
		return "@" + a.BotUsername
	}
	if a.BotName != "" {
		return a.BotName
	}
	return strconv.FormatInt(a.BotUserID, 10)
}

func (a *Account) WebhookUnhealthy(pendingThreshold int) bool {
	if a.WebhookLastError != "" {
		return true
	}
	return pendingThreshold > 0 && a.WebhookPendingCount >= pendingThreshold
}

type Contact struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	AccountID   string `json:"accountId"`

	TGUserID int64  `json:"tgUserId"`
	TGChatID int64  `json:"tgChatId"`
	ChatType string `json:"chatType"`

	Username         string     `json:"username,omitempty"`
	FirstName        string     `json:"firstName,omitempty"`
	LastName         string     `json:"lastName,omitempty"`
	LanguageCode     string     `json:"languageCode,omitempty"`
	IsPremium        bool       `json:"isPremium"`
	PhotoFileID      string     `json:"photoFileId,omitempty"`
	PhotoURL         string     `json:"photoUrl,omitempty"`
	ProfileFetchedAt *time.Time `json:"profileFetchedAt,omitempty"`

	PhoneNumber   *string    `json:"phoneNumber,omitempty"`
	PhoneSharedAt *time.Time `json:"phoneSharedAt,omitempty"`
	LeadID        *string    `json:"leadId,omitempty"`

	Blocked   bool       `json:"blocked"`
	BlockedAt *time.Time `json:"blockedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c *Contact) DisplayName() string {
	full := strings.TrimSpace(strings.TrimSpace(c.FirstName) + " " + strings.TrimSpace(c.LastName))
	if full != "" {
		return full
	}
	if u := strings.TrimSpace(c.Username); u != "" {
		return "@" + u
	}
	return strconv.FormatInt(c.TGUserID, 10)
}

func (c *Contact) Handle() string {
	if u := strings.TrimSpace(c.Username); u != "" {
		return "@" + u
	}
	if c.PhoneNumber != nil && strings.TrimSpace(*c.PhoneNumber) != "" {
		return strings.TrimSpace(*c.PhoneNumber)
	}
	return ""
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
	AccountID   string `json:"accountId"`
	ContactID   string `json:"contactId"`

	TGChatID int64  `json:"tgChatId"`
	ChatType string `json:"chatType"`

	BusinessConnectionID *string `json:"businessConnectionId,omitempty"`

	ConversationStatus string     `json:"conversationStatus,omitempty"`
	CloseSource        string     `json:"closeSource,omitempty"`
	CloseReason        string     `json:"closeReason,omitempty"`
	ClosedAt           *time.Time `json:"closedAt,omitempty"`
	AutomationEnabled  *bool      `json:"automationEnabled,omitempty"`

	LastMessageAt         *time.Time `json:"lastMessageAt,omitempty"`
	LastCustomerMessageAt *time.Time `json:"lastCustomerMessageAt,omitempty"`
	LastAgentMessageAt    *time.Time `json:"lastAgentMessageAt,omitempty"`

	StartPayload *string `json:"startPayload,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c *Conversation) BusinessWindowOpen(now time.Time) (bool, *time.Time) {
	if c.LastCustomerMessageAt == nil {
		return false, nil
	}
	expires := c.LastCustomerMessageAt.Add(BusinessMessagingWindow)
	return now.Before(expires), &expires
}

func (c *Conversation) IsPrivate() bool {
	return c.ChatType == "" || c.ChatType == ChatTypePrivate
}

const (
	ChatTypePrivate    = "private"
	ChatTypeGroup      = "group"
	ChatTypeSupergroup = "supergroup"
	ChatTypeChannel    = "channel"
)

type DeepLink struct {
	Token       string `json:"token"`
	AccountID   string `json:"accountId"`
	WorkspaceID string `json:"workspaceId"`

	LeadID       *string `json:"leadId,omitempty"`
	CampaignID   *string `json:"campaignId,omitempty"`
	AgentID      *string `json:"agentId,omitempty"`
	DepartmentID *string `json:"departmentId,omitempty"`
	Label        string  `json:"label,omitempty"`

	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	UsedAt    *time.Time `json:"usedAt,omitempty"`
	UseCount  int        `json:"useCount"`

	CreatedAt time.Time `json:"createdAt"`
}

func (d *DeepLink) Expired(now time.Time) bool {
	return d.ExpiresAt != nil && now.After(*d.ExpiresAt)
}

func (d *DeepLink) URL(botUsername string) string {
	return "https://t.me/" + strings.TrimPrefix(botUsername, "@") + "?start=" + d.Token
}

func ValidDeepLinkToken(token string) bool {
	if token == "" || len(token) > MaxDeepLinkPayload {
		return false
	}
	for _, r := range token {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}
