package telegram

import (
	"context"
	"strings"
	"time"
)

type BotProfile struct {
	BotUserID            int64
	Username             string
	FirstName            string
	CanJoinGroups        bool
	CanReadAllGroup      bool
	CanConnectToBusiness bool
}

type WebhookInfo struct {
	URL              string
	PendingCount     int
	LastErrorDate    *time.Time
	LastErrorMessage string
	MaxConnections   int
	AllowedUpdates   []string
}

func AllowedUpdates() []string {
	return []string{
		"message",
		"edited_message",
		"callback_query",
		"my_chat_member",
		"business_connection",
		"business_message",
		"edited_business_message",
		"deleted_business_messages",
	}
}

const DefaultMaxConnections = 40

type WebhookConfig struct {
	URL                string
	SecretToken        string
	MaxConnections     int
	AllowedUpdates     []string
	DropPendingUpdates bool
}

type SendTextInput struct {
	ChatID               int64
	Text                 string
	ParseMode            string
	ReplyToMessageID     int64
	BusinessConnectionID string
	ReplyMarkup          string
}

type MediaKind string

const (
	MediaPhoto    MediaKind = "photo"
	MediaVideo    MediaKind = "video"
	MediaAudio    MediaKind = "audio"
	MediaVoice    MediaKind = "voice"
	MediaDocument MediaKind = "document"
)

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

type SendResult struct {
	MessageID int64
	ChatID    int64
	Date      time.Time
	FileID    string
}

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

type RemoteFile struct {
	FileID   string
	Path     string
	Size     int64
	TooLarge bool
}

type BotAPI interface {
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
	SetMessageReaction(ctx context.Context, token string, chatID, messageID int64, emoji string) error
	ReadBusinessMessage(ctx context.Context, token, businessConnectionID string, chatID, messageID int64) error
	AnswerCallbackQuery(ctx context.Context, token, callbackQueryID, text string) error

	GetFile(ctx context.Context, token, fileID string) (*RemoteFile, error)
	DownloadFile(ctx context.Context, token, filePath string) (data []byte, contentType string, err error)
	GetUserProfilePhotoFileID(ctx context.Context, token string, userID int64) (string, error)
}

type APIError struct {
	HTTPStatus      int
	Code            int
	Description     string
	RetryAfter      int
	MigrateToChatID int64
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	return "telegram api error " + itoa(e.Code) + ": " + e.Description
}

func (e *APIError) Retryable() bool {
	if e == nil {
		return false
	}
	if e.Code == 429 || e.RetryAfter > 0 {
		return true
	}
	return e.HTTPStatus >= 500
}

func (e *APIError) NeedsReconnect() bool {
	return e != nil && e.Code == 401
}

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

func (e *APIError) Migrated() bool { return e != nil && e.MigrateToChatID != 0 }

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
