package telegram

import (
	"context"
	"strings"
	"time"
)

// ---------------------------------------------------------------- identity

// BotProfile is getMe's answer.
type BotProfile struct {
	BotUserID            int64
	Username             string
	FirstName            string
	CanJoinGroups        bool
	CanReadAllGroup      bool
	CanConnectToBusiness bool
}

// WebhookInfo is getWebhookInfo's answer, narrowed to the fields that tell us
// whether we are losing data.
type WebhookInfo struct {
	URL              string
	PendingCount     int
	LastErrorDate    *time.Time
	LastErrorMessage string
	MaxConnections   int
	AllowedUpdates   []string
}

// ---------------------------------------------------------------- webhooks

// AllowedUpdates is the subscription list registered with setWebhook.
//
// Two Bot API rules make this list load-bearing:
//
//   - "Specify an empty list to receive all update types except chat_member,
//     message_reaction, and message_reaction_count." Passing nothing would
//     therefore subscribe us to inline queries, polls and chat boosts we do not
//     handle, wasting the delivery budget.
//   - The parameter "doesn't affect updates created before the call", so a
//     narrowed list still lets a short tail of unwanted updates through. The
//     consumer must tolerate anything, which is why unknown kinds are logged
//     rather than treated as errors.
//
// message_reaction is deliberately ABSENT: the docs require the bot to be "an
// administrator in the chat" to receive it, and a private chat has no
// administrators. Subscribing would imply an inbound-reaction feature the
// platform may never deliver.
func AllowedUpdates() []string {
	return []string{
		"message",
		"edited_message",
		"callback_query",
		// Private chats deliver this only on block/unblock, which is exactly the
		// signal bot mode uses in place of a messaging window.
		"my_chat_member",
		// Business mode. Subscribing in both modes costs nothing and means a
		// workspace that later connects a business account starts receiving
		// immediately instead of after a re-registration.
		"business_connection",
		"business_message",
		"edited_business_message",
		"deleted_business_messages",
	}
}

// DefaultMaxConnections is setWebhook's max_connections. The documented default
// is 40; it is stated explicitly so the delivery concurrency is a decision
// rather than an accident.
const DefaultMaxConnections = 40

// WebhookConfig is one setWebhook call.
type WebhookConfig struct {
	URL            string
	SecretToken    string
	MaxConnections int
	AllowedUpdates []string
	// DropPendingUpdates discards the backlog. Only ever set deliberately: after
	// a long outage the backlog is stale, but discarding it is data loss.
	DropPendingUpdates bool
}

// ---------------------------------------------------------------- sending

// SendTextInput is one sendMessage call.
type SendTextInput struct {
	ChatID int64
	Text   string
	// ParseMode is HTML rather than MarkdownV2. MarkdownV2 requires escaping a
	// long list of characters, and a single stray underscore in a customer's name
	// fails the whole send.
	ParseMode string
	// ReplyToMessageID quotes an earlier message.
	ReplyToMessageID int64
	// BusinessConnectionID sends on behalf of a connected business account.
	BusinessConnectionID string
	// ReplyMarkup is an optional inline keyboard, already JSON-encoded.
	ReplyMarkup string
}

// MediaKind is the send method to use.
type MediaKind string

const (
	MediaPhoto    MediaKind = "photo"
	MediaVideo    MediaKind = "video"
	MediaAudio    MediaKind = "audio"
	MediaVoice    MediaKind = "voice"
	MediaDocument MediaKind = "document"
)

// SendMediaInput is one media send.
//
// Exactly one of FileID, URL or Bytes is used, in that order of preference:
// a cached file_id has no size limit at all, a URL caps at 5MB (photos) / 20MB,
// and a multipart upload caps at 10MB / 50MB. Preferring the cache first is what
// makes repeat sends of the same asset free.
type SendMediaInput struct {
	ChatID   int64
	Kind     MediaKind
	FileID   string
	URL      string
	Bytes    []byte
	FileName string
	MIMEType string
	Caption  string

	ReplyToMessageID     int64
	BusinessConnectionID string
}

// SendResult is the provider's acknowledgement.
//
// Unlike Meta, Telegram answers a send synchronously with the full Message, so
// the provider id is known immediately and there is no echo webhook to
// reconcile against.
type SendResult struct {
	MessageID int64
	ChatID    int64
	Date      time.Time
	// FileID is the id Telegram assigned to an uploaded asset, worth caching.
	FileID string
}

// ChatAction is a sendChatAction value. The status expires in five seconds or
// less, so a long-running reply must re-issue it.
type ChatAction string

const (
	ActionTyping        ChatAction = "typing"
	ActionUploadPhoto   ChatAction = "upload_photo"
	ActionUploadVideo   ChatAction = "upload_video"
	ActionUploadVoice   ChatAction = "upload_voice"
	ActionUploadDoc     ChatAction = "upload_document"
	ActionRecordVoice   ChatAction = "record_voice"
	ActionChooseSticker ChatAction = "choose_sticker"
)

// RemoteFile is getFile's answer plus the download decision.
type RemoteFile struct {
	FileID string
	Path   string
	Size   int64
	// TooLarge is true when Telegram reports a size above the bot download
	// ceiling. The caller must render a placeholder rather than attempt a fetch
	// that can only fail.
	TooLarge bool
}

// BotAPI is the outbound Telegram surface.
//
// Every method takes the bot token explicitly rather than being constructed per
// account: a workspace can connect several bots, and binding the token to the
// call is what guarantees a reply leaves from the same bot the message arrived
// on.
type BotAPI interface {
	// GetMe validates a token and identifies the bot.
	GetMe(ctx context.Context, token string) (*BotProfile, error)

	SetWebhook(ctx context.Context, token string, cfg WebhookConfig) error
	DeleteWebhook(ctx context.Context, token string, dropPending bool) error
	GetWebhookInfo(ctx context.Context, token string) (*WebhookInfo, error)

	SendText(ctx context.Context, token string, in SendTextInput) (*SendResult, error)
	SendMedia(ctx context.Context, token string, in SendMediaInput) (*SendResult, error)

	EditText(ctx context.Context, token string, chatID, messageID int64, text, parseMode, businessConnectionID string) error
	DeleteMessage(ctx context.Context, token string, chatID, messageID int64) error
	DeleteBusinessMessages(ctx context.Context, token, businessConnectionID string, messageIDs []int64) error

	SendChatAction(ctx context.Context, token string, chatID int64, action ChatAction, businessConnectionID string) error
	// SetMessageReaction sets or clears our reaction. Bots may set at most one
	// reaction per message; an empty emoji clears it.
	SetMessageReaction(ctx context.Context, token string, chatID, messageID int64, emoji string) error
	// ReadBusinessMessage marks a customer message read on the owner's behalf.
	// Business mode only, and only with the can_read_messages right.
	ReadBusinessMessage(ctx context.Context, token, businessConnectionID string, chatID, messageID int64) error
	// AnswerCallbackQuery must be called for every callback_query, or the
	// client's button spins until it times out.
	AnswerCallbackQuery(ctx context.Context, token, callbackQueryID, text string) error

	GetFile(ctx context.Context, token, fileID string) (*RemoteFile, error)
	// DownloadFile fetches a resolved file path. It refuses paths whose size
	// exceeds the bot download ceiling rather than starting a doomed transfer.
	DownloadFile(ctx context.Context, token, filePath string) (data []byte, contentType string, err error)
	// GetUserProfilePhotoFileID returns the newest profile photo's file id, or ""
	// when the user has none or hides them.
	GetUserProfilePhotoFileID(ctx context.Context, token string, userID int64) (string, error)
}

// ---------------------------------------------------------------- errors

// APIError is a structured Bot API failure.
//
// Telegram is markedly better than Meta here: a flood wait answers with an
// explicit `retry_after` in seconds, and a migrated group answers with the new
// chat id. Preserving both is what lets retry and recovery be correct instead of
// guessed.
type APIError struct {
	HTTPStatus  int
	Code        int
	Description string
	// RetryAfter is ResponseParameters.retry_after: "the number of seconds left
	// to wait before the request can be repeated".
	RetryAfter int
	// MigrateToChatID is ResponseParameters.migrate_to_chat_id: the group became
	// a supergroup and has a new id. Ignoring it kills the conversation
	// silently.
	MigrateToChatID int64
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	return "telegram api error " + itoa(e.Code) + ": " + e.Description
}

// Retryable reports whether repeating the call could succeed.
func (e *APIError) Retryable() bool {
	if e == nil {
		return false
	}
	if e.Code == 429 || e.RetryAfter > 0 {
		return true
	}
	return e.HTTPStatus >= 500
}

// NeedsReconnect reports whether the credential itself is dead. 401 is the only
// way a Telegram token dies: it does not expire, it is revoked in BotFather.
func (e *APIError) NeedsReconnect() bool {
	return e != nil && e.Code == 401
}

// BlockedByUser reports whether the contact has made the bot unable to reach
// them. Telegram spells several distinct situations as 403; all of them mean the
// same thing for the CRM, and none of them is retryable.
func (e *APIError) BlockedByUser() bool {
	if e == nil || e.Code != 403 {
		return false
	}
	d := strings.ToLower(e.Description)
	return strings.Contains(d, "blocked") ||
		strings.Contains(d, "user is deactivated") ||
		strings.Contains(d, "chat not found") ||
		strings.Contains(d, "bot was kicked") ||
		strings.Contains(d, "initiate conversation")
}

// Migrated reports whether the chat moved to a new id.
func (e *APIError) Migrated() bool { return e != nil && e.MigrateToChatID != 0 }

// RetryDelay is how long to wait before repeating, honouring Telegram's own
// answer instead of an invented backoff.
func (e *APIError) RetryDelay() time.Duration {
	if e == nil || e.RetryAfter <= 0 {
		return 0
	}
	return time.Duration(e.RetryAfter) * time.Second
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
