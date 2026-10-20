package tiktok

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	eventReceive         = "im_receive_msg"
	eventReceiveEU       = "im_receive_msg_eu"
	eventSend            = "im_send_msg"
	eventReferral        = "im_referral_msg"
	eventMarkRead        = "im_mark_read_msg"
	eventAutoConfig      = "im_auto_message_config_update"
	eventAutoAudit       = "im_auto_message_audit_update"
	eventHighIntent      = "im_receive_high_intent_comment"
	eventCommentUpdate   = "comment.update"
	roleBusinessAccount  = "business_account"
	maxMessageIDLength   = 128
	maxWebhookTextLength = 64 * 1024
)

type EventKind string

const (
	EventMessageReceived   EventKind = "message_received"
	EventMessageReceivedEU EventKind = "message_received_eu"
	EventMessageSent       EventKind = "message_sent"
	EventReferral          EventKind = "referral"
	EventRead              EventKind = "read"
	EventComment           EventKind = "comment"
	EventAutoMessageConfig EventKind = "auto_message_config"
	EventAutoMessageAudit  EventKind = "auto_message_audit"
	EventHighIntentComment EventKind = "high_intent_comment"
)

type MessageType string

const (
	MessageText      MessageType = "text"
	MessageImage     MessageType = "image"
	MessageVideo     MessageType = "video"
	MessageSharePost MessageType = "share_post"
	MessageEmoji     MessageType = "emoji"
	MessageSticker   MessageType = "sticker"
	MessageReaction  MessageType = "reaction"
	MessageTemplate  MessageType = "template"
	MessageOther     MessageType = "other"
)

func parseMessageType(raw string) MessageType {
	switch t := MessageType(strings.ToLower(strings.TrimSpace(raw))); t {
	case MessageText, MessageImage, MessageVideo, MessageSharePost, MessageEmoji, MessageSticker, MessageReaction, MessageTemplate:
		return t
	}
	return MessageOther
}

type MessageSource string

const (
	SourceApp     MessageSource = "APP"
	SourceWeb     MessageSource = "WEB"
	SourceAPI     MessageSource = "API"
	SourceOthers  MessageSource = "OTHERS"
	SourceUnknown MessageSource = "UNKNOWN_SOURCE"
)

func parseSource(raw string) MessageSource {
	switch s := MessageSource(strings.ToUpper(strings.TrimSpace(raw))); s {
	case SourceApp, SourceWeb, SourceAPI, SourceOthers:
		return s
	}
	return SourceUnknown
}

type Reaction struct {
	Operation         string    `json:"operation"`
	Type              string    `json:"type"`
	Emoji             string    `json:"emoji,omitempty"`
	OriginalMessageID string    `json:"originalMessageId,omitempty"`
	ReactedAt         time.Time `json:"reactedAt"`
}

type Card struct {
	Title   string       `json:"title"`
	Buttons []CardButton `json:"buttons"`
}

type CardButton struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type Message struct {
	ConversationID string
	MessageID      string
	PersonID       string
	PersonUsername string
	IsFollower     *bool

	Type                MessageType
	Text                string
	MediaID             string
	SharedVideoID       string
	SharedEmbedURL      string
	StickerURL          string
	EmojiURL            string
	Reactions           []Reaction
	ReferencedMessageID string
	ButtonID            string
	Card                *Card
	AutoMessageType     string
	Source              MessageSource

	SentAt time.Time
}

type ReferralEvent struct {
	ConversationID string
	PersonID       string
	PersonUsername string
	IsFollower     *bool
	Referral       Referral
}

type ReadEvent struct {
	ConversationID string
	PersonID       string
	ReadUpTo       time.Time
}

type CommentEvent struct {
	CommentID       string
	VideoID         string
	ParentCommentID string
	Kind            CommentKind
	Action          CommentAction
	PersonID        string
	Text            string
}

type AutoMessageEvent struct {
	ID          string
	Type        string
	Action      string
	AuditStatus string
	Text        string
	Question    string
	Answer      string
	Title       string
}

type Event struct {
	Kind          EventKind
	AccountOpenID string
	OccurredAt    time.Time

	Message     *Message
	Referral    *ReferralEvent
	Read        *ReadEvent
	Comment     *CommentEvent
	AutoMessage *AutoMessageEvent
}

func (e Event) IdempotencyKey() string {
	switch e.Kind {
	case EventMessageReceived:
		return "tt:msg:in:" + e.Message.MessageID
	case EventMessageSent:
		return "tt:msg:out:" + e.Message.MessageID
	case EventMessageReceivedEU:
		return "tt:eu:" + e.AccountOpenID + ":" + millis(e.OccurredAt)
	case EventReferral:
		return "tt:ref:" + e.Referral.ConversationID + ":" + millis(e.OccurredAt)
	case EventRead:
		return "tt:read:" + e.Read.ConversationID + ":" + millis(e.Read.ReadUpTo)
	case EventComment:
		return "tt:cmt:" + e.Comment.CommentID + ":" + string(e.Comment.Action) + ":" + millis(e.OccurredAt)
	case EventAutoMessageConfig:
		return "tt:auto:" + e.AutoMessage.ID + ":" + e.AutoMessage.Action + ":" + millis(e.OccurredAt)
	case EventAutoMessageAudit:
		return "tt:audit:" + e.AutoMessage.ID + ":" + e.AutoMessage.AuditStatus + ":" + millis(e.OccurredAt)
	case EventHighIntentComment:
		return "tt:hic:" + e.AccountOpenID + ":" + millis(e.OccurredAt)
	}
	return ""
}

func millis(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }

type Envelope struct {
	ClientKey  string `json:"client_key"`
	Event      string `json:"event"`
	CreateTime int64  `json:"create_time"`
	UserOpenID string `json:"user_openid"`
	Content    string `json:"content"`
}

func DecodeEnvelope(raw []byte) (Envelope, error) {
	var wire struct {
		ClientKey  string          `json:"client_key"`
		Event      string          `json:"event"`
		CreateTime json.Number     `json:"create_time"`
		UserOpenID string          `json:"user_openid"`
		Content    json.RawMessage `json:"content"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return Envelope{}, fmt.Errorf("%w: %v", ErrInvalidEnvelope, err)
	}
	if strings.TrimSpace(wire.Event) == "" {
		return Envelope{}, fmt.Errorf("%w: event is missing", ErrInvalidEnvelope)
	}
	if !ValidOpaqueID(wire.UserOpenID) {
		return Envelope{}, fmt.Errorf("%w: user_openid is missing or malformed", ErrInvalidEnvelope)
	}
	created, err := wire.CreateTime.Int64()
	if err != nil || created < 0 {
		return Envelope{}, fmt.Errorf("%w: create_time %q", ErrInvalidEnvelope, wire.CreateTime)
	}
	var content string
	if len(wire.Content) == 0 || wire.Content[0] != '"' {
		return Envelope{}, fmt.Errorf("%w: content must be a JSON string", ErrInvalidEnvelope)
	}
	if err := json.Unmarshal(wire.Content, &content); err != nil {
		return Envelope{}, fmt.Errorf("%w: content: %v", ErrInvalidEnvelope, err)
	}
	return Envelope{ClientKey: wire.ClientKey, Event: wire.Event, CreateTime: created, UserOpenID: wire.UserOpenID, Content: content}, nil
}

func decodeStrict(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing data after the JSON value")
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err == nil {
		return fmt.Errorf("trailing data after the JSON value")
	}
	return nil
}

type userRef struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

type messageContent struct {
	From             string   `json:"from"`
	To               string   `json:"to"`
	UniqueIdentifier string   `json:"unique_identifier"`
	FromUser         *userRef `json:"from_user"`
	ToUser           *userRef `json:"to_user"`
	ConversationID   string   `json:"conversation_id"`
	MessageID        string   `json:"message_id"`
	Timestamp        int64    `json:"timestamp"`
	Type             string   `json:"type"`
	IsFollower       *bool    `json:"is_follower"`
	AutoMessageType  string   `json:"auto_message_type"`

	Text *struct {
		Body string `json:"body"`
	} `json:"text"`
	Image *struct {
		MediaID string `json:"media_id"`
	} `json:"image"`
	Video *struct {
		MediaID string `json:"media_id"`
	} `json:"video"`
	SharePost *struct {
		EmbedURL string `json:"embed_url"`
		VideoID  string `json:"video_id"`
	} `json:"share_post"`
	Sticker *struct {
		URL string `json:"url"`
	} `json:"sticker"`
	Emoji *struct {
		URL string `json:"url"`
	} `json:"emoji"`
	Reaction []struct {
		Operation     string `json:"operation"`
		Type          string `json:"type"`
		Emoji         string `json:"emoji"`
		OriginalMsgID string `json:"original_msg_id"`
		Timestamp     int64  `json:"timestamp"`
	} `json:"reaction"`
	Template *struct {
		Type     string `json:"type"`
		Elements []struct {
			Title   string `json:"title"`
			Buttons []struct {
				Text string `json:"text"`
				ID   string `json:"id"`
			} `json:"buttons"`
		} `json:"elements"`
	} `json:"template"`
	ReferencedMessageInfo *struct {
		ReferencedMessageID string `json:"referenced_message_id"`
	} `json:"referenced_message_info"`
	ReplySourcePayload *struct {
		ReplySourceUniqueID string `json:"reply_source_unique_id"`
	} `json:"reply_source_payload"`
	MessageTag *struct {
		Source string `json:"source"`
	} `json:"message_tag"`
	Referral *struct {
		Source string `json:"source"`
		Ad     *struct {
			AdvertiserID string `json:"advertiser_id"`
			AdID         string `json:"ad_id"`
			AdName       string `json:"ad_name"`
			EmbedURL     string `json:"embed_url"`
			Timestamp    int64  `json:"timestamp"`
		} `json:"ad"`
		ShortLink *struct {
			Ref              string `json:"ref"`
			PrefilledMessage string `json:"prefilled_message"`
		} `json:"short_link"`
	} `json:"referral"`
	Read *struct {
		LastReadTimestamp int64 `json:"last_read_timestamp"`
	} `json:"read"`
}

type commentContent struct {
	CommentID        NumericID `json:"comment_id"`
	VideoID          NumericID `json:"video_id"`
	ParentCommentID  NumericID `json:"parent_comment_id"`
	CommentType      string    `json:"comment_type"`
	CommentAction    string    `json:"comment_action"`
	UniqueIdentifier string    `json:"unique_identifier"`
	Timestamp        int64     `json:"timestamp"`
	Text             string    `json:"text"`
}

type autoMessageContent struct {
	AutoMessageID     string `json:"auto_message_id"`
	AutoMessageType   string `json:"auto_message_type"`
	AutoMessageAction string `json:"auto_message_action"`
	AuditStatus       string `json:"audit_status"`
	Timestamp         int64  `json:"timestamp"`
	WelcomeMessage    *struct {
		Content string `json:"content"`
	} `json:"welcome_message"`
	SuggestedQuestion *struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
	} `json:"suggested_question"`
	ChatPrompt *struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	} `json:"chat_prompt"`
}

func Normalize(env Envelope) (Event, error) {
	base := Event{AccountOpenID: env.UserOpenID}
	envelopeTime := time.Unix(env.CreateTime, 0).UTC()
	switch env.Event {
	case eventReceive, eventSend:
		return normalizeMessage(base, env, envelopeTime)
	case eventReceiveEU:
		var c struct {
			Timestamp int64 `json:"timestamp"`
		}
		if err := decodeContent(env.Content, &c); err != nil {
			return Event{}, err
		}
		base.Kind = EventMessageReceivedEU
		base.OccurredAt = eventTime(c.Timestamp, envelopeTime)
		return base, nil
	case eventReferral:
		return normalizeReferral(base, env, envelopeTime)
	case eventMarkRead:
		return normalizeRead(base, env, envelopeTime)
	case eventCommentUpdate:
		return normalizeComment(base, env, envelopeTime)
	case eventAutoConfig, eventAutoAudit:
		return normalizeAutoMessage(base, env, envelopeTime)
	case eventHighIntent:
		base.Kind = EventHighIntentComment
		base.OccurredAt = envelopeTime
		return base, nil
	}
	return Event{}, fmt.Errorf("%w: %q", ErrUnknownEvent, env.Event)
}

func decodeContent(content string, out any) error {
	if len(content) > maxWebhookTextLength {
		return fmt.Errorf("%w: content is too large", ErrInvalidContent)
	}
	if err := decodeStrict([]byte(content), out); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidContent, err)
	}
	return nil
}

func eventTime(milli int64, fallback time.Time) time.Time {
	if milli > 0 {
		return time.UnixMilli(milli).UTC()
	}
	return fallback
}

func normalizeMessage(base Event, env Envelope, envelopeTime time.Time) (Event, error) {
	var c messageContent
	if err := decodeContent(env.Content, &c); err != nil {
		return Event{}, err
	}
	received := env.Event == eventReceive
	business := c.ToUser
	personUsername := c.From
	if !received {
		business = c.FromUser
		personUsername = c.To
	}
	if business != nil && business.Role == roleBusinessAccount && business.ID != "" && business.ID != env.UserOpenID {
		return Event{}, fmt.Errorf("%w: message addressed to another account", ErrInvalidContent)
	}
	if !ValidOpaqueID(c.ConversationID) {
		return Event{}, fmt.Errorf("%w: conversation_id is missing or malformed", ErrInvalidContent)
	}
	if c.MessageID == "" || len(c.MessageID) > maxMessageIDLength || !ValidOpaqueID(c.MessageID) {
		return Event{}, fmt.Errorf("%w: message_id is missing or malformed", ErrInvalidContent)
	}
	if !ValidOpaqueID(c.UniqueIdentifier) {
		return Event{}, fmt.Errorf("%w: unique_identifier is missing or malformed", ErrInvalidContent)
	}
	m := &Message{
		ConversationID:  c.ConversationID,
		MessageID:       c.MessageID,
		PersonID:        c.UniqueIdentifier,
		PersonUsername:  strings.TrimSpace(personUsername),
		IsFollower:      c.IsFollower,
		Type:            parseMessageType(c.Type),
		AutoMessageType: strings.TrimSpace(c.AutoMessageType),
		Source:          SourceUnknown,
		SentAt:          eventTime(c.Timestamp, envelopeTime),
	}
	if c.MessageTag != nil {
		m.Source = parseSource(c.MessageTag.Source)
	}
	if c.Text != nil {
		m.Text = c.Text.Body
	}
	switch {
	case c.Image != nil:
		m.MediaID = c.Image.MediaID
	case c.Video != nil:
		m.MediaID = c.Video.MediaID
	}
	if c.SharePost != nil {
		m.SharedVideoID = c.SharePost.VideoID
		m.SharedEmbedURL = c.SharePost.EmbedURL
	}
	if c.Sticker != nil {
		m.StickerURL = c.Sticker.URL
	}
	if c.Emoji != nil {
		m.EmojiURL = c.Emoji.URL
	}
	for _, r := range c.Reaction {
		m.Reactions = append(m.Reactions, Reaction{
			Operation:         r.Operation,
			Type:              r.Type,
			Emoji:             r.Emoji,
			OriginalMessageID: r.OriginalMsgID,
			ReactedAt:         eventTime(r.Timestamp, m.SentAt),
		})
	}
	if c.ReferencedMessageInfo != nil {
		m.ReferencedMessageID = c.ReferencedMessageInfo.ReferencedMessageID
	}
	if c.ReplySourcePayload != nil {
		m.ButtonID = c.ReplySourcePayload.ReplySourceUniqueID
	}
	if c.Template != nil && len(c.Template.Elements) > 0 {
		element := c.Template.Elements[0]
		card := &Card{Title: element.Title}
		for _, b := range element.Buttons {
			card.Buttons = append(card.Buttons, CardButton{ID: b.ID, Text: b.Text})
		}
		m.Card = card
	}
	base.Kind = EventMessageSent
	if received {
		base.Kind = EventMessageReceived
	}
	base.OccurredAt = m.SentAt
	base.Message = m
	return base, nil
}

func normalizeReferral(base Event, env Envelope, envelopeTime time.Time) (Event, error) {
	var c messageContent
	if err := decodeContent(env.Content, &c); err != nil {
		return Event{}, err
	}
	if !ValidOpaqueID(c.ConversationID) || !ValidOpaqueID(c.UniqueIdentifier) {
		return Event{}, fmt.Errorf("%w: referral without a conversation or a person", ErrInvalidContent)
	}
	if c.Referral == nil {
		return Event{}, fmt.Errorf("%w: referral is missing", ErrInvalidContent)
	}
	ref := Referral{Source: strings.TrimSpace(c.Referral.Source)}
	switch {
	case c.Referral.Ad != nil:
		ref.AdvertiserID = c.Referral.Ad.AdvertiserID
		ref.AdID = c.Referral.Ad.AdID
		ref.AdName = c.Referral.Ad.AdName
		ref.EmbedURL = c.Referral.Ad.EmbedURL
		ref.ReferredAtMilli = c.Referral.Ad.Timestamp
	case c.Referral.ShortLink != nil:
		ref.Ref = c.Referral.ShortLink.Ref
		ref.PrefilledText = c.Referral.ShortLink.PrefilledMessage
	}
	base.Kind = EventReferral
	base.OccurredAt = eventTime(c.Timestamp, envelopeTime)
	base.Referral = &ReferralEvent{
		ConversationID: c.ConversationID,
		PersonID:       c.UniqueIdentifier,
		PersonUsername: strings.TrimSpace(c.From),
		IsFollower:     c.IsFollower,
		Referral:       ref,
	}
	return base, nil
}

func normalizeRead(base Event, env Envelope, envelopeTime time.Time) (Event, error) {
	var c messageContent
	if err := decodeContent(env.Content, &c); err != nil {
		return Event{}, err
	}
	if !ValidOpaqueID(c.ConversationID) || c.Read == nil || c.Read.LastReadTimestamp <= 0 {
		return Event{}, fmt.Errorf("%w: read without a conversation or a timestamp", ErrInvalidContent)
	}
	base.Kind = EventRead
	base.OccurredAt = eventTime(c.Timestamp, envelopeTime)
	base.Read = &ReadEvent{
		ConversationID: c.ConversationID,
		PersonID:       c.UniqueIdentifier,
		ReadUpTo:       time.UnixMilli(c.Read.LastReadTimestamp).UTC(),
	}
	return base, nil
}

func normalizeComment(base Event, env Envelope, envelopeTime time.Time) (Event, error) {
	var c commentContent
	if err := decodeContent(env.Content, &c); err != nil {
		return Event{}, err
	}
	if c.CommentID == "" || c.VideoID == "" {
		return Event{}, fmt.Errorf("%w: comment or video id is missing", ErrInvalidContent)
	}
	action, err := ParseCommentAction(c.CommentAction)
	if err != nil {
		return Event{}, err
	}
	kind := CommentKind(c.CommentType)
	switch kind {
	case KindComment:
	case KindReply:
		if c.ParentCommentID == "" {
			return Event{}, fmt.Errorf("%w: reply without a parent comment", ErrInvalidContent)
		}
	default:
		return Event{}, fmt.Errorf("%w: comment type %q", ErrInvalidContent, c.CommentType)
	}
	person := strings.TrimSpace(c.UniqueIdentifier)
	if person != "" && !ValidOpaqueID(person) {
		return Event{}, fmt.Errorf("%w: unique_identifier is malformed", ErrInvalidContent)
	}
	if action == CommentInserted && person == "" {
		return Event{}, fmt.Errorf("%w: a new comment without its author", ErrInvalidContent)
	}
	base.Kind = EventComment
	base.OccurredAt = eventTime(c.Timestamp, envelopeTime)
	base.Comment = &CommentEvent{
		CommentID:       c.CommentID.String(),
		VideoID:         c.VideoID.String(),
		ParentCommentID: c.ParentCommentID.String(),
		Kind:            kind,
		Action:          action,
		PersonID:        person,
		Text:            c.Text,
	}
	return base, nil
}

func normalizeAutoMessage(base Event, env Envelope, envelopeTime time.Time) (Event, error) {
	var c autoMessageContent
	if err := decodeContent(env.Content, &c); err != nil {
		return Event{}, err
	}
	if !ValidOpaqueID(c.AutoMessageID) {
		return Event{}, fmt.Errorf("%w: auto_message_id is missing", ErrInvalidContent)
	}
	auto := &AutoMessageEvent{
		ID:          c.AutoMessageID,
		Type:        strings.TrimSpace(c.AutoMessageType),
		Action:      strings.TrimSpace(c.AutoMessageAction),
		AuditStatus: strings.TrimSpace(c.AuditStatus),
	}
	switch {
	case c.WelcomeMessage != nil:
		auto.Text = c.WelcomeMessage.Content
	case c.SuggestedQuestion != nil:
		auto.Question = c.SuggestedQuestion.Question
		auto.Answer = c.SuggestedQuestion.Answer
	case c.ChatPrompt != nil:
		auto.Title = c.ChatPrompt.Title
		auto.Text = c.ChatPrompt.Content
	}
	base.Kind = EventAutoMessageConfig
	if env.Event == eventAutoAudit {
		base.Kind = EventAutoMessageAudit
		if auto.AuditStatus == "" {
			return Event{}, fmt.Errorf("%w: audit without a status", ErrInvalidContent)
		}
	} else if auto.Action == "" {
		return Event{}, fmt.Errorf("%w: config change without an action", ErrInvalidContent)
	}
	base.OccurredAt = eventTime(c.Timestamp, envelopeTime)
	base.AutoMessage = auto
	return base, nil
}
