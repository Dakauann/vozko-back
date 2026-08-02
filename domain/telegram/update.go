package telegram

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidUpdate = errors.New("telegram: invalid update payload")

// ---------------------------------------------------------------- raw payload
//
// Only the fields we act on are modelled. The Bot API grows several new update
// kinds and message fields per release (10.0 → 10.2 added three update kinds in
// three months), so everything else is deliberately ignored rather than
// enumerated — and an unrecognised update is logged, never dropped silently.

// Update is one webhook POST body. "At most one of the optional fields can be
// present in any given update."
//
// Note what is NOT here: any identification of the bot. Unlike Meta, whose
// entry[].id routes to a tenant, a Telegram update says nothing about which bot
// it is for. Tenancy therefore comes from the request URL (bot mode) or from
// business_connection_id (business mode), never from the body.
type Update struct {
	UpdateID int64 `json:"update_id"`

	Message       *Message `json:"message,omitempty"`
	EditedMessage *Message `json:"edited_message,omitempty"`

	BusinessConnection      *BusinessConnection      `json:"business_connection,omitempty"`
	BusinessMessage         *Message                 `json:"business_message,omitempty"`
	EditedBusinessMessage   *Message                 `json:"edited_business_message,omitempty"`
	DeletedBusinessMessages *BusinessMessagesDeleted `json:"deleted_business_messages,omitempty"`

	CallbackQuery *CallbackQuery     `json:"callback_query,omitempty"`
	MyChatMember  *ChatMemberUpdated `json:"my_chat_member,omitempty"`
}

// User is the Bot API User object.
//
// ID is int64 because Telegram ids have "at most 52 significant bits" — a 32-bit
// round trip silently corrupts them, which is the single most common Telegram
// integration bug.
type User struct {
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name,omitempty"`
	Username     string `json:"username,omitempty"`
	LanguageCode string `json:"language_code,omitempty"`
	IsPremium    bool   `json:"is_premium,omitempty"`
}

type Chat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title,omitempty"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

// Message is the Bot API Message object, narrowed to what the CRM records.
type Message struct {
	// MessageID is "Unique message identifier INSIDE THIS CHAT" — it is not
	// globally unique, so every persisted provider id pairs it with the chat id.
	MessageID int64 `json:"message_id"`
	From      *User `json:"from,omitempty"`
	Chat      Chat  `json:"chat"`
	Date      int64 `json:"date"`
	EditDate  int64 `json:"edit_date,omitempty"`

	// BusinessConnectionID is set on messages belonging to a connected business
	// account, and is how such a message is routed to a tenant.
	BusinessConnectionID string `json:"business_connection_id,omitempty"`
	// SenderBusinessBot is present only on outgoing messages the bot sent on the
	// business account's behalf — one half of the direction test.
	SenderBusinessBot *User `json:"sender_business_bot,omitempty"`
	// IsFromOffline marks an automatic away or greeting message, which must not
	// be treated as an operator's reply.
	IsFromOffline bool `json:"is_from_offline,omitempty"`

	Text     string          `json:"text,omitempty"`
	Caption  string          `json:"caption,omitempty"`
	Entities []MessageEntity `json:"entities,omitempty"`

	// ReplyMarkup is the inline keyboard attached to this message. On a
	// callback_query it is what maps the received payload back to the LABEL the
	// contact actually tapped — the payload is an internal id, and showing it in
	// the transcript (or handing it to an AI agent as the customer's words) is
	// wrong and actively misleading.
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`

	ReplyToMessage *Message `json:"reply_to_message,omitempty"`
	// MediaGroupID ties the parts of an album together. Telegram delivers each
	// part as a SEPARATE update, so an album is several messages that share this
	// id, never one message with several attachments.
	MediaGroupID string `json:"media_group_id,omitempty"`

	// Photo is an array of sizes for one image, smallest first.
	Photo     []PhotoSize `json:"photo,omitempty"`
	Video     *FileMeta   `json:"video,omitempty"`
	Audio     *FileMeta   `json:"audio,omitempty"`
	Voice     *FileMeta   `json:"voice,omitempty"`
	Document  *FileMeta   `json:"document,omitempty"`
	Animation *FileMeta   `json:"animation,omitempty"`
	VideoNote *FileMeta   `json:"video_note,omitempty"`
	Sticker   *Sticker    `json:"sticker,omitempty"`

	Contact  *Contact_ `json:"contact,omitempty"`
	Location *Location `json:"location,omitempty"`
}

type MessageEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

type PhotoSize struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FileSize     int64  `json:"file_size,omitempty"`
}

// FileMeta covers video/audio/voice/document/animation/video_note, which share a
// shape.
//
// MIMEType and FileName are captured HERE rather than at download time because
// getFile "may not preserve the original file name and MIME type".
type FileMeta struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileName     string `json:"file_name,omitempty"`
	MIMEType     string `json:"mime_type,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
	Duration     int    `json:"duration,omitempty"`
}

type Sticker struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Emoji        string `json:"emoji,omitempty"`
	SetName      string `json:"set_name,omitempty"`
	IsAnimated   bool   `json:"is_animated,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
}

// Contact_ is the Bot API Contact object. The trailing underscore avoids
// colliding with our own domain Contact; this one is a wire type.
type Contact_ struct {
	PhoneNumber string `json:"phone_number"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name,omitempty"`
	// UserID is present only when the contact is a Telegram user. It is what
	// proves the sharer shared their OWN number rather than a third party's.
	UserID int64 `json:"user_id,omitempty"`
}

type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type BusinessConnection struct {
	ID         string          `json:"id"`
	User       User            `json:"user"`
	UserChatID int64           `json:"user_chat_id"`
	Date       int64           `json:"date"`
	Rights     *BusinessRights `json:"rights,omitempty"`
	IsEnabled  bool            `json:"is_enabled"`
}

type BusinessMessagesDeleted struct {
	BusinessConnectionID string  `json:"business_connection_id"`
	Chat                 Chat    `json:"chat"`
	MessageIDs           []int64 `json:"message_ids"`
}

// InlineKeyboardMarkup is the button grid attached to a message.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard,omitempty"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

// LabelFor resolves the visible label of the button carrying this payload.
//
// Returns "" when the keyboard is absent or has no such button — the caller
// falls back to the payload, which is worse but never empty.
func (m *InlineKeyboardMarkup) LabelFor(data string) string {
	if m == nil || data == "" {
		return ""
	}
	for _, row := range m.InlineKeyboard {
		for _, b := range row {
			if b.CallbackData == data {
				return b.Text
			}
		}
	}
	return ""
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

type ChatMemberUpdated struct {
	Chat          Chat        `json:"chat"`
	From          User        `json:"from"`
	Date          int64       `json:"date"`
	OldChatMember *ChatMember `json:"old_chat_member,omitempty"`
	NewChatMember *ChatMember `json:"new_chat_member,omitempty"`
}

type ChatMember struct {
	Status string `json:"status"`
	User   User   `json:"user"`
}

// DecodeUpdate parses one webhook body.
//
// Telegram posts a single JSON object per request — no batching, no array
// wrapper, no multi-tenant fan-in. That is a genuine simplification over Meta,
// and the decoder stays strict rather than inventing tolerance the wire does not
// need.
func DecodeUpdate(body []byte) (*Update, error) {
	var u Update
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidUpdate, err)
	}
	if u.UpdateID == 0 {
		return nil, fmt.Errorf("%w: missing update_id", ErrInvalidUpdate)
	}
	return &u, nil
}

// ---------------------------------------------------------------- normalized

// EventKind classifies an update after normalization, so consumers switch on an
// explicit kind instead of re-sniffing raw JSON.
type EventKind string

const (
	EventInboundMessage EventKind = "inbound_message"
	// EventOutboundMessage is a message the business account sent — either by us
	// through the bot, or by the owner from their own phone. Business mode only.
	EventOutboundMessage    EventKind = "outbound_message"
	EventEditedMessage      EventKind = "edited_message"
	EventDeletedMessages    EventKind = "deleted_messages"
	EventCallbackQuery      EventKind = "callback_query"
	EventContactShared      EventKind = "contact_shared"
	EventBlocked            EventKind = "blocked"
	EventUnblocked          EventKind = "unblocked"
	EventBusinessConnection EventKind = "business_connection"
	EventUnknown            EventKind = "unknown"
)

// Attachment is one normalized inbound file.
type Attachment struct {
	Kind     MediaKind
	FileID   string
	FileName string
	MIMEType string
	Size     int64
	Duration int
	// TooLarge is set when Telegram already told us the file exceeds the bot
	// download ceiling, so the handler can render a placeholder WITHOUT calling
	// getFile — a call that can only fail.
	TooLarge bool
	// Emoji carries a sticker's emoji, which is the only renderable thing about
	// a sticker we cannot download.
	Emoji string
}

// Event is a normalized update. One raw update yields at most one event, except
// deleted_business_messages, which carries a list.
type Event struct {
	Kind EventKind
	// UpdateID is the natural idempotency key: Telegram assigns exactly one per
	// event, unlike a Meta `mid` which recurs across five event kinds.
	UpdateID int64
	// IdempotencyKey is scoped by account because update_id is per bot.
	IdempotencyKey string

	Timestamp time.Time

	// BusinessConnectionID routes business-mode events to a tenant.
	BusinessConnectionID string

	ChatID   int64
	ChatType string
	// Contact identity, taken straight from the update so a first message yields
	// a named contact with no extra API call.
	From *User

	MessageID int64
	Text      string
	// ReplyToMessageID is the quoted message, when there is one.
	ReplyToMessageID int64
	MediaGroupID     string
	Attachments      []Attachment

	// StartPayload is the deep-link token from "/start <payload>".
	StartPayload string
	// IsCommand marks a message whose text begins with a bot command.
	IsCommand bool

	// SharedContact is a consented phone share.
	SharedContact *Contact_
	Location      *Location

	// CallbackQueryID must be answered or the client's button spins forever.
	CallbackQueryID string
	CallbackData    string

	// DeletedMessageIDs is populated for EventDeletedMessages.
	DeletedMessageIDs []int64

	// Connection is populated for EventBusinessConnection.
	Connection *BusinessConnection

	// IsAutomatic marks an away/greeting message the account sent by itself.
	IsAutomatic bool

	// Raw carries an unrecognised update verbatim so it is logged, not lost.
	Raw json.RawMessage
}

// NormalizeUpdate converts one raw update into a normalized event.
//
// accountID scopes the idempotency key, because update_id is unique per bot and
// two workspaces' bots will collide on it otherwise.
func NormalizeUpdate(accountID string, u *Update, raw json.RawMessage) *Event {
	if u == nil {
		return nil
	}

	base := func(kind EventKind, suffix string) *Event {
		return &Event{
			Kind:           kind,
			UpdateID:       u.UpdateID,
			IdempotencyKey: idemKey(accountID, suffix, u.UpdateID),
		}
	}

	switch {
	case u.Message != nil:
		ev := base(EventInboundMessage, "message")
		fillFromMessage(ev, u.Message)
		// A shared contact is a distinct CRM action — it links the conversation to
		// a lead — so it is classified rather than buried in the message body.
		if u.Message.Contact != nil {
			ev.Kind = EventContactShared
			ev.SharedContact = u.Message.Contact
		}
		return ev

	case u.EditedMessage != nil:
		ev := base(EventEditedMessage, "edit")
		fillFromMessage(ev, u.EditedMessage)
		return ev

	case u.BusinessMessage != nil:
		msg := u.BusinessMessage
		// Direction is NOT implied by the update kind here: business_message
		// carries both the customer's messages and the account owner's own
		// replies. sender_business_bot is present only on messages the bot sent,
		// and the owner's own messages arrive with from.id == the business user.
		// The caller compares against the connection's user id; this only
		// classifies what the payload can prove on its own.
		kind := EventInboundMessage
		if msg.SenderBusinessBot != nil {
			kind = EventOutboundMessage
		}
		ev := base(kind, "business_message")
		fillFromMessage(ev, msg)
		if msg.Contact != nil && kind == EventInboundMessage {
			ev.Kind = EventContactShared
			ev.SharedContact = msg.Contact
		}
		return ev

	case u.EditedBusinessMessage != nil:
		ev := base(EventEditedMessage, "business_edit")
		fillFromMessage(ev, u.EditedBusinessMessage)
		return ev

	case u.DeletedBusinessMessages != nil:
		d := u.DeletedBusinessMessages
		ev := base(EventDeletedMessages, "business_delete")
		ev.BusinessConnectionID = d.BusinessConnectionID
		ev.ChatID = d.Chat.ID
		ev.ChatType = d.Chat.Type
		ev.DeletedMessageIDs = d.MessageIDs
		return ev

	case u.BusinessConnection != nil:
		ev := base(EventBusinessConnection, "business_connection")
		ev.BusinessConnectionID = u.BusinessConnection.ID
		ev.Connection = u.BusinessConnection
		ev.Timestamp = unixToTime(u.BusinessConnection.Date)
		ev.ChatID = u.BusinessConnection.UserChatID
		from := u.BusinessConnection.User
		ev.From = &from
		return ev

	case u.CallbackQuery != nil:
		cq := u.CallbackQuery
		ev := base(EventCallbackQuery, "callback")
		ev.CallbackQueryID = cq.ID
		ev.CallbackData = cq.Data
		from := cq.From
		ev.From = &from
		if cq.Message != nil {
			ev.ChatID = cq.Message.Chat.ID
			ev.ChatType = cq.Message.Chat.Type
			ev.MessageID = cq.Message.MessageID
			ev.BusinessConnectionID = cq.Message.BusinessConnectionID
			ev.Timestamp = unixToTime(cq.Message.Date)
			// Text is what a HUMAN — or an AI agent reading the transcript — sees
			// as the contact's message, so it must be the button's label. The
			// payload stays in CallbackData, which is what routing keys on.
			ev.Text = cq.Message.ReplyMarkup.LabelFor(cq.Data)
		}
		if ev.Text == "" {
			ev.Text = cq.Data
		}
		if ev.ChatID == 0 {
			ev.ChatID = cq.From.ID
			ev.ChatType = ChatTypePrivate
		}
		return ev

	case u.MyChatMember != nil:
		m := u.MyChatMember
		// For private chats this update "is received only when the bot is blocked
		// or unblocked by the user", which is precisely the outbound gate in bot
		// mode.
		kind := EventUnknown
		if m.NewChatMember != nil {
			switch m.NewChatMember.Status {
			case "kicked", "left":
				kind = EventBlocked
			case "member", "administrator", "creator", "restricted":
				kind = EventUnblocked
			}
		}
		ev := base(kind, "chat_member")
		ev.ChatID = m.Chat.ID
		ev.ChatType = m.Chat.Type
		from := m.From
		ev.From = &from
		ev.Timestamp = unixToTime(m.Date)
		if kind == EventUnknown {
			ev.Raw = raw
		}
		return ev
	}

	ev := base(EventUnknown, "unknown")
	ev.Raw = raw
	return ev
}

// fillFromMessage projects the shared message fields onto an event.
func fillFromMessage(ev *Event, msg *Message) {
	ev.ChatID = msg.Chat.ID
	ev.ChatType = msg.Chat.Type
	ev.MessageID = msg.MessageID
	ev.BusinessConnectionID = msg.BusinessConnectionID
	ev.MediaGroupID = msg.MediaGroupID
	ev.IsAutomatic = msg.IsFromOffline
	ev.From = msg.From

	// An edit carries edit_date; using it keeps the CRM's ordering honest.
	ts := msg.Date
	if msg.EditDate > 0 {
		ts = msg.EditDate
	}
	ev.Timestamp = unixToTime(ts)

	text := msg.Text
	if text == "" {
		text = msg.Caption
	}
	ev.Text = strings.TrimSpace(text)

	if msg.ReplyToMessage != nil {
		ev.ReplyToMessageID = msg.ReplyToMessage.MessageID
	}
	if msg.Location != nil {
		ev.Location = msg.Location
	}

	ev.IsCommand, ev.StartPayload = parseStart(msg)
	ev.Attachments = attachmentsOf(msg)
}

// parseStart extracts a deep-link payload from "/start <payload>".
//
// Telegram delivers the payload as ordinary message text, so this is the only
// place attribution can be recovered. The command is matched on the entity
// rather than on a string prefix, so a message that merely mentions "/start" is
// not mistaken for one.
func parseStart(msg *Message) (isCommand bool, payload string) {
	if msg == nil || msg.Text == "" {
		return false, ""
	}
	for _, e := range msg.Entities {
		if e.Type != "bot_command" || e.Offset != 0 {
			continue
		}
		isCommand = true
		end := e.Offset + e.Length
		if end > len(msg.Text) {
			end = len(msg.Text)
		}
		command := msg.Text[e.Offset:end]
		// In groups the command arrives as "/start@thebot".
		if at := strings.IndexByte(command, '@'); at >= 0 {
			command = command[:at]
		}
		if !strings.EqualFold(command, "/start") {
			return true, ""
		}
		rest := strings.TrimSpace(msg.Text[end:])
		if ValidDeepLinkToken(rest) {
			return true, rest
		}
		return true, ""
	}
	return false, ""
}

// attachmentsOf normalizes a message's media.
//
// A Telegram message carries at most ONE attachment — albums arrive as separate
// updates sharing a media_group_id — so this returns a slice only for symmetry
// with channels that do batch, and to keep the handler's loop identical.
func attachmentsOf(msg *Message) []Attachment {
	tooLarge := func(size int64) bool { return size > MaxDownloadBytes }

	switch {
	case len(msg.Photo) > 0:
		// Sizes are ordered smallest first; the last is the original.
		best := msg.Photo[len(msg.Photo)-1]
		return []Attachment{{
			Kind:     MediaPhoto,
			FileID:   best.FileID,
			MIMEType: "image/jpeg",
			Size:     best.FileSize,
			TooLarge: tooLarge(best.FileSize),
		}}

	case msg.Video != nil:
		return []Attachment{fileAttachment(MediaVideo, msg.Video, "video/mp4")}
	case msg.VideoNote != nil:
		return []Attachment{fileAttachment(MediaVideo, msg.VideoNote, "video/mp4")}
	case msg.Voice != nil:
		return []Attachment{fileAttachment(MediaVoice, msg.Voice, "audio/ogg")}
	case msg.Audio != nil:
		return []Attachment{fileAttachment(MediaAudio, msg.Audio, "audio/mpeg")}
	case msg.Animation != nil:
		return []Attachment{fileAttachment(MediaVideo, msg.Animation, "video/mp4")}
	case msg.Document != nil:
		return []Attachment{fileAttachment(MediaDocument, msg.Document, "application/octet-stream")}

	case msg.Sticker != nil:
		// A sticker is a real message with no useful body. It is recorded so the
		// transcript is not silently missing a turn.
		return []Attachment{{
			Kind:     MediaDocument,
			FileID:   msg.Sticker.FileID,
			MIMEType: "image/webp",
			Size:     msg.Sticker.FileSize,
			TooLarge: tooLarge(msg.Sticker.FileSize),
			Emoji:    msg.Sticker.Emoji,
		}}
	}
	return nil
}

func fileAttachment(kind MediaKind, f *FileMeta, fallbackMIME string) Attachment {
	mime := f.MIMEType
	if mime == "" {
		mime = fallbackMIME
	}
	return Attachment{
		Kind:     kind,
		FileID:   f.FileID,
		FileName: f.FileName,
		MIMEType: mime,
		Size:     f.FileSize,
		Duration: f.Duration,
		TooLarge: f.FileSize > MaxDownloadBytes,
	}
}

// SortByUpdateID orders a batch of events.
//
// "This identifier ... allows you to ignore repeated updates or to restore the
// correct update sequence, SHOULD THEY GET OUT OF ORDER." Arrival order is
// therefore not the transcript order, and ordering here is what keeps a
// conversation readable under concurrent delivery.
func SortByUpdateID(events []*Event) {
	sort.SliceStable(events, func(i, j int) bool { return events[i].UpdateID < events[j].UpdateID })
}

// ProviderMessageID renders the durable id stored as
// conversation_messages.external_message_id.
//
// message_id is unique only INSIDE a chat, so pairing it with the chat id is
// what makes the partial unique index on (entry_type, external_message_id)
// actually prevent duplicates instead of rejecting unrelated messages.
func ProviderMessageID(chatID, messageID int64) string {
	return strconv.FormatInt(chatID, 10) + ":" + strconv.FormatInt(messageID, 10)
}

// ParseProviderMessageID is ProviderMessageID's inverse, for the edit and delete
// paths that need the pair back.
func ParseProviderMessageID(id string) (chatID, messageID int64, ok bool) {
	parts := strings.SplitN(id, ":", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	c, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	m, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return c, m, true
}

func idemKey(accountID, kind string, updateID int64) string {
	return "tg:" + accountID + ":" + kind + ":" + strconv.FormatInt(updateID, 10)
}

func unixToTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}
