// Package telegram holds the Telegram channel's contracts and rules.
//
// It deliberately contains no HTTP: the Bot API details live in infra/telegram.
// What is here is the vocabulary the rest of the system reasons about, accounts,
// contacts, conversations, deep links, plus the rules that are genuinely
// Telegram's and cannot be inferred from a generic messaging model.
//
// Two of those rules shape everything downstream:
//
//  1. A bot cannot open a conversation. Sending to a user who never started the
//     bot fails with "bot can't initiate conversation with a user". Our
//     conversations only exist because an inbound message created one, so the
//     rule is satisfied by construction, the real outbound gate is whether the
//     contact has since BLOCKED the bot.
//  2. There are two connection modes with different messaging rules. See Mode.
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

// MaxTextRunes is Telegram's documented limit: "1-4096 characters after entities
// parsing". Note CHARACTERS, Instagram's equivalent limit is in BYTES, and
// applying the wrong one silently truncates or over-accepts.
const MaxTextRunes = 4096

// MaxCaptionRunes is the documented media-caption limit: "0-1024 characters
// after entities parsing".
const MaxCaptionRunes = 1024

// MaxDownloadBytes is the hard ceiling on inbound media: "For the moment, bots
// can download files of up to 20MB in size."
//
// This is the channel's honest limitation, the analogue of Instagram's
// impossible post-deletion. A customer can send a file we simply cannot fetch,
// and the UI must say so rather than render an empty bubble. A self-hosted Local
// Bot API Server removes the limit; until then the placeholder is the product.
const MaxDownloadBytes int64 = 20 << 20

// Upload ceilings, from "Sending files". Sending by file_id has no limit at all,
// which is why uploaded assets are cached and reused.
const (
	MaxUploadPhotoBytes int64 = 10 << 20
	MaxUploadOtherBytes int64 = 50 << 20
)

// BusinessMessagingWindow is the reply window in business mode. The Bot API
// defines can_reply as "the bot can send and edit messages in the private chats
// that had incoming messages in the last 24 hours".
const BusinessMessagingWindow = 24 * time.Hour

// EditWindow / DeleteWindow are Telegram's documented mutation limits.
//
// deleteMessage: "A message can only be deleted if it was sent less than 48
// hours ago." editMessageText carries the same 48h bound for business messages
// the bot did not send; the bot's OWN messages have no documented edit limit,
// but we apply the same bound so the UI has one rule instead of two.
const (
	EditWindow   = 48 * time.Hour
	DeleteWindow = 48 * time.Hour
)

// MaxCallbackDataBytes is the documented inline-button payload limit: "Data to
// be sent in a callback query to the bot when the button is pressed, 1-64
// bytes". Bytes, not characters, an option id with accented text overflows it
// sooner than it looks.
const MaxCallbackDataBytes = 64

// MaxInlineKeyboardButtons is OUR cap on options in one inline keyboard, not
// Telegram's. The Bot API documents inline_keyboard only as "Array of Array of
// InlineKeyboardButton" and states no bound. A limit is still needed so a
// generated workflow config cannot produce a message that is unusable on a
// phone, and so the workflow editor has a concrete number to show the author.
const MaxInlineKeyboardButtons = 100

// InlineKeyboardColumns is how many buttons share a row.
//
// One per row is deliberate: option labels come from workflow authors and are
// not length-bounded by Telegram, so a two-column layout truncates unpredictably
// across devices. A single column always renders the full label.
const InlineKeyboardColumns = 1

// MaxDeepLinkPayload is the documented start-parameter limit: "The parameter can
// be up to 64 characters long", from the set A-Z a-z 0-9 _ -.
const MaxDeepLinkPayload = 64

// Rate limits, per bot. From the Bot FAQ: "In a single chat, avoid sending more
// than one message per second"; "bots are not able to broadcast more than about
// 30 messages per second".
const (
	PerChatMessagesPerSecond = 1
	PerBotMessagesPerSecond  = 30
)

// Mode is how a workspace connected Telegram. The two modes are not variants of
// one flow: they differ in who owns the chat, how onboarding works, and whether
// there is a messaging window.
type Mode string

const (
	// ModeBot is the classic Bot API: customers message @yourcompany_bot, the bot
	// owns the chat, and there is NO messaging window. The bot cannot open a
	// conversation, but every conversation we hold was opened by the customer.
	ModeBot Mode = "BOT"

	// ModeBusiness is Telegram Business: a bot connected to a real user account
	// answers that account's DMs. The customer is messaging a person. This mode
	// reintroduces a 24h window via BusinessBotRights.can_reply, and the rights
	// can be changed or revoked by the account owner at any time.
	ModeBusiness Mode = "BUSINESS"
)

func (m Mode) Valid() bool { return m == ModeBot || m == ModeBusiness }

type Status string

const (
	StatusPending Status = "PENDING"
	StatusActive  Status = "ACTIVE"
	// StatusTokenInvalid means getMe returned 401: the token was revoked in
	// BotFather. Only a fresh token recovers it.
	StatusTokenInvalid Status = "TOKEN_INVALID"
	// StatusWebhookFailing means Telegram cannot deliver to us.
	//
	// This is the channel's data-loss alarm, not a cosmetic health flag:
	// "Incoming updates are stored on the server until the bot receives them
	// either way, but they will not be kept longer than 24 hours", and there is
	// no history API to backfill from. An outage longer than a day loses
	// messages permanently.
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

// CanTransitionTo guards the lifecycle, following the Instagram precedent of
// having a guard from day one rather than bare field assignment.
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
		// Re-registering the webhook recovers it without new credentials.
		return next == StatusActive || next == StatusTokenInvalid || next == StatusRevoked
	case StatusTokenInvalid:
		return next == StatusActive || next == StatusRevoked
	case StatusRevoked:
		return next == StatusActive
	}
	return false
}

// BusinessRights mirrors the Bot API's BusinessBotRights object.
//
// Every field is presence-only upstream (the type is literally `True`), so a
// missing field means "not granted". The owner can change them at any moment and
// we learn only from the next business_connection update, which is why they are
// stored verbatim and re-read rather than assumed from onboarding.
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

// Account is a connected Telegram bot. It doubles as the config carrier for its
// conversations, the role whatsapp_campaigns plays for WhatsApp and
// instagram_accounts plays for Instagram, which is why the automation fields
// live here. Telegram has no campaign concept: there is no cold outbound to
// blast and no template to carry.
type Account struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspaceId"`
	DepartmentID *string `json:"departmentId,omitempty"`

	Mode Mode `json:"mode"`

	// BotUserID is the bot's own Telegram id, from getMe. Telegram ids carry up
	// to 52 significant bits, so this is int64 everywhere; a 32-bit round trip
	// is the classic Telegram integration bug.
	BotUserID   int64  `json:"botUserId"`
	BotUsername string `json:"botUsername"`
	BotName     string `json:"botName,omitempty"`
	// CanConnectToBusiness is getMe's can_connect_to_business: whether this bot
	// may be attached to a Telegram Business account at all.
	CanConnectToBusiness bool `json:"canConnectToBusiness"`

	// BotToken never leaves the server and is never serialized. Encrypted at
	// rest: a Telegram token does not expire and grants total control of the bot,
	// so plaintext storage would be strictly worse than the Meta equivalent.
	BotToken string `json:"-"`
	// WebhookSecret is echoed by Telegram in X-Telegram-Bot-Api-Secret-Token. It
	// is the ONLY authenticity control the channel has, there is no body
	// signature, so it is treated as a credential and compared in constant time.
	WebhookSecret string `json:"-"`

	WebhookSetAt *time.Time `json:"webhookSetAt,omitempty"`
	// WebhookPendingCount mirrors getWebhookInfo.pending_update_count. Rising
	// values mean Telegram is failing to reach us; see StatusWebhookFailing.
	WebhookPendingCount int        `json:"webhookPendingCount"`
	WebhookLastError    string     `json:"webhookLastError,omitempty"`
	WebhookLastErrorAt  *time.Time `json:"webhookLastErrorAt,omitempty"`
	WebhookCheckedAt    *time.Time `json:"webhookCheckedAt,omitempty"`

	// Business-mode identity. Empty in bot mode.
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

// CanSend reports whether this account is usable for outbound at all.
//
// A webhook-failing account can still SEND, the failure is inbound-only, so it
// is deliberately not excluded here. Only a dead token or a revoked account is.
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

// Rights returns the granted business rights, never nil, so callers can read a
// field without a presence check. In bot mode everything the bot can do it can
// do unconditionally, so the zero value would be wrong, bot mode is handled by
// the caller before it reaches here.
func (a *Account) Rights() BusinessRights {
	if a.BusinessRights == nil {
		return BusinessRights{}
	}
	return *a.BusinessRights
}

// DisplayName is what the CRM shows for the account.
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

// WebhookUnhealthy reports whether Telegram is struggling to deliver.
//
// The threshold is deliberately low. Because undelivered updates are discarded
// after 24 hours and cannot be recovered, a backlog is an alarm rather than a
// statistic.
func (a *Account) WebhookUnhealthy(pendingThreshold int) bool {
	if a.WebhookLastError != "" {
		return true
	}
	return pendingThreshold > 0 && a.WebhookPendingCount >= pendingThreshold
}

// Contact is a person who messaged one of our bots.
//
// Unlike an Instagram IGSID, a Telegram user id is GLOBAL, the same human has
// the same id for every bot. Identity is still scoped to (account, user) so one
// workspace can never read another's contact row, and so the same person talking
// to two of a workspace's bots keeps two independent conversations.
type Contact struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	AccountID   string `json:"accountId"`

	TGUserID int64 `json:"tgUserId"`
	// TGChatID equals TGUserID for private chats, but is stored explicitly:
	// group chats differ, and a group upgraded to a supergroup gets a NEW chat id
	// delivered as ResponseParameters.migrate_to_chat_id.
	TGChatID int64  `json:"tgChatId"`
	ChatType string `json:"chatType"`

	Username     string `json:"username,omitempty"`
	FirstName    string `json:"firstName,omitempty"`
	LastName     string `json:"lastName,omitempty"`
	LanguageCode string `json:"languageCode,omitempty"`
	IsPremium    bool   `json:"isPremium"`
	// PhotoFileID is durable; the download URL it resolves to is not, so only the
	// id is stored and the URL is fetched on demand.
	PhotoFileID      string     `json:"photoFileId,omitempty"`
	PhotoURL         string     `json:"photoUrl,omitempty"`
	ProfileFetchedAt *time.Time `json:"profileFetchedAt,omitempty"`

	// PhoneNumber is only ever populated from an explicit consent tap on a
	// request_contact keyboard button. Telegram never volunteers it.
	PhoneNumber   *string    `json:"phoneNumber,omitempty"`
	PhoneSharedAt *time.Time `json:"phoneSharedAt,omitempty"`
	// LeadID bridges this contact to a CRM lead once a phone number makes the
	// match possible.
	LeadID *string `json:"leadId,omitempty"`

	// Blocked is set from my_chat_member, which for private chats fires only on
	// block/unblock. In bot mode this, not a clock, is what closes the
	// composer.
	Blocked   bool       `json:"blocked"`
	BlockedAt *time.Time `json:"blockedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// DisplayName prefers the real name, falling back to the handle and then the id,
// so a row is never blank.
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

// Handle is the @username when there is one, else the shared phone, else empty.
// It fills the CRM's "number" slot, which is what an operator searches by.
func (c *Contact) Handle() string {
	if u := strings.TrimSpace(c.Username); u != "" {
		return "@" + u
	}
	if c.PhoneNumber != nil && strings.TrimSpace(*c.PhoneNumber) != "" {
		return strings.TrimSpace(*c.PhoneNumber)
	}
	return ""
}

// ProfileIsStale reports whether the cached profile should be refreshed.
// Enrichment is lazy: profile reads compete with the per-bot send budget.
func (c *Contact) ProfileIsStale(now time.Time, ttl time.Duration) bool {
	if c.ProfileFetchedAt == nil {
		return true
	}
	return now.Sub(*c.ProfileFetchedAt) > ttl
}

// Conversation is the ENTRY, what the CRM treats as a conversation. It carries
// the same state contract as whatsapp_campaign_entries and
// instagram_conversations, so labels, stages, opportunities and inbox assignment
// (all keyed on entry_id + entry_type) work with no change to those subsystems.
type Conversation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	AccountID   string `json:"accountId"`
	ContactID   string `json:"contactId"`

	TGChatID int64  `json:"tgChatId"`
	ChatType string `json:"chatType"`

	// BusinessConnectionID records which connection this conversation arrived
	// through, so a reply is sent on behalf of the right account.
	BusinessConnectionID *string `json:"businessConnectionId,omitempty"`

	ConversationStatus string     `json:"conversationStatus,omitempty"`
	CloseSource        string     `json:"closeSource,omitempty"`
	CloseReason        string     `json:"closeReason,omitempty"`
	ClosedAt           *time.Time `json:"closedAt,omitempty"`
	AutomationEnabled  *bool      `json:"automationEnabled,omitempty"`

	LastMessageAt         *time.Time `json:"lastMessageAt,omitempty"`
	LastCustomerMessageAt *time.Time `json:"lastCustomerMessageAt,omitempty"`
	LastAgentMessageAt    *time.Time `json:"lastAgentMessageAt,omitempty"`

	// StartPayload is the deep-link token the contact arrived with, kept so the
	// attribution is visible on the conversation itself and not only in the
	// deep-link table.
	StartPayload *string `json:"startPayload,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// BusinessWindowOpen reports whether the business-mode 24h window is open, and
// when it closes. Bot mode has no window at all, which is why this is not called
// there, see the channel adapter.
func (c *Conversation) BusinessWindowOpen(now time.Time) (bool, *time.Time) {
	if c.LastCustomerMessageAt == nil {
		return false, nil
	}
	expires := c.LastCustomerMessageAt.Add(BusinessMessagingWindow)
	return now.Before(expires), &expires
}

// IsPrivate reports whether this is a 1:1 chat. Group chats are stored but do
// not run automation: with privacy mode on, a bot sees only commands and replies
// there, so the transcript is partial by construction and an agent answering
// from it would be answering half a conversation.
func (c *Conversation) IsPrivate() bool {
	return c.ChatType == "" || c.ChatType == ChatTypePrivate
}

// Chat types, from the Bot API's Chat.type.
const (
	ChatTypePrivate    = "private"
	ChatTypeGroup      = "group"
	ChatTypeSupergroup = "supergroup"
	ChatTypeChannel    = "channel"
)

// DeepLink is a t.me/<bot>?start=<token> attribution record.
//
// The start payload is capped at 64 characters, which is not enough to carry
// real ids, so the link carries an opaque token and this row carries the
// attribution it resolves to.
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

// Expired reports whether the link may no longer be redeemed.
func (d *DeepLink) Expired(now time.Time) bool {
	return d.ExpiresAt != nil && now.After(*d.ExpiresAt)
}

// URL renders the shareable link for a bot.
func (d *DeepLink) URL(botUsername string) string {
	return "https://t.me/" + strings.TrimPrefix(botUsername, "@") + "?start=" + d.Token
}

// ValidDeepLinkToken reports whether a token is one Telegram will actually
// deliver: "The parameter can be up to 64 characters long", from A-Z a-z 0-9 _ -.
//
// Telegram silently drops a link whose payload contains anything else, so
// generating one is a bug that shows up as "the deep link just opens a normal
// chat".
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
