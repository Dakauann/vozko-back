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

type Message struct {
	MessageID int64 `json:"message_id"`
	From      *User `json:"from,omitempty"`
	Chat      Chat  `json:"chat"`
	Date      int64 `json:"date"`
	EditDate  int64 `json:"edit_date,omitempty"`

	BusinessConnectionID string `json:"business_connection_id,omitempty"`
	SenderBusinessBot    *User  `json:"sender_business_bot,omitempty"`
	IsFromOffline        bool   `json:"is_from_offline,omitempty"`

	Text     string          `json:"text,omitempty"`
	Caption  string          `json:"caption,omitempty"`
	Entities []MessageEntity `json:"entities,omitempty"`

	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`

	ReplyToMessage *Message `json:"reply_to_message,omitempty"`
	MediaGroupID   string   `json:"media_group_id,omitempty"`

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

type Contact_ struct {
	PhoneNumber string `json:"phone_number"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name,omitempty"`
	UserID      int64  `json:"user_id,omitempty"`
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

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard,omitempty"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

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

type EventKind string

const (
	EventInboundMessage     EventKind = "inbound_message"
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

type Attachment struct {
	Kind     MediaKind
	FileID   string
	FileName string
	MIMEType string
	Size     int64
	Duration int
	TooLarge bool
	Emoji    string
}

type Event struct {
	Kind           EventKind
	UpdateID       int64
	IdempotencyKey string

	Timestamp time.Time

	BusinessConnectionID string

	ChatID   int64
	ChatType string
	From     *User

	MessageID        int64
	Text             string
	ReplyToMessageID int64
	MediaGroupID     string
	Attachments      []Attachment

	StartPayload string
	IsCommand    bool

	SharedContact *Contact_
	Location      *Location

	CallbackQueryID string
	CallbackData    string

	DeletedMessageIDs []int64

	Connection *BusinessConnection

	IsAutomatic bool

	Raw json.RawMessage
}

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

func fillFromMessage(ev *Event, msg *Message) {
	ev.ChatID = msg.Chat.ID
	ev.ChatType = msg.Chat.Type
	ev.MessageID = msg.MessageID
	ev.BusinessConnectionID = msg.BusinessConnectionID
	ev.MediaGroupID = msg.MediaGroupID
	ev.IsAutomatic = msg.IsFromOffline
	ev.From = msg.From

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

func attachmentsOf(msg *Message) []Attachment {
	tooLarge := func(size int64) bool { return size > MaxDownloadBytes }

	switch {
	case len(msg.Photo) > 0:
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

func SortByUpdateID(events []*Event) {
	sort.SliceStable(events, func(i, j int) bool { return events[i].UpdateID < events[j].UpdateID })
}

func ProviderMessageID(botUserID, chatID, messageID int64) string {
	return strconv.FormatInt(botUserID, 10) + ":" +
		strconv.FormatInt(chatID, 10) + ":" +
		strconv.FormatInt(messageID, 10)
}

func ParseProviderMessageID(id string) (chatID, messageID int64, ok bool) {
	parts := strings.Split(id, ":")
	switch len(parts) {
	case 2:
	case 3:
		parts = parts[1:]
	default:
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
