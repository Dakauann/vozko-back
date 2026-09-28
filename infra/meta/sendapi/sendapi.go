package sendapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"vozko/infra/meta"
)

const (
	MessagingResponse = "RESPONSE"
	MessagingUpdate   = "UPDATE"
	MessagingTag      = "MESSAGE_TAG"

	TagHumanAgent = "HUMAN_AGENT"

	ActionTypingOn  = "typing_on"
	ActionTypingOff = "typing_off"
	ActionMarkSeen  = "mark_seen"
	ActionReact     = "react"
	ActionUnreact   = "unreact"
)

type Recipient struct {
	ID        string `json:"id,omitempty"`
	CommentID string `json:"comment_id,omitempty"`
	PostID    string `json:"post_id,omitempty"`
}

func ToUser(id string) Recipient           { return Recipient{ID: id} }
func ToComment(commentID string) Recipient { return Recipient{CommentID: commentID} }
func ToPost(postID string) Recipient       { return Recipient{PostID: postID} }

type QuickReply struct {
	ContentType string `json:"content_type"`
	Title       string `json:"title"`
	Payload     string `json:"payload"`
}

type Button struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Payload string `json:"payload,omitempty"`
	URL     string `json:"url,omitempty"`
}

type AttachmentPayload struct {
	URL          string   `json:"url,omitempty"`
	AttachmentID string   `json:"attachment_id,omitempty"`
	IsReusable   *bool    `json:"is_reusable,omitempty"`
	TemplateType string   `json:"template_type,omitempty"`
	Text         string   `json:"text,omitempty"`
	Buttons      []Button `json:"buttons,omitempty"`
}

type Attachment struct {
	Type    string            `json:"type"`
	Payload AttachmentPayload `json:"payload"`
}

type Message struct {
	Text         string       `json:"text,omitempty"`
	Attachment   *Attachment  `json:"attachment,omitempty"`
	QuickReplies []QuickReply `json:"quick_replies,omitempty"`
	Metadata     string       `json:"metadata,omitempty"`
}

type ReplyTo struct {
	MID string `json:"mid"`
}

type ActionPayload struct {
	MessageID string `json:"message_id"`
	Reaction  string `json:"reaction,omitempty"`
}

type Envelope struct {
	Recipient     Recipient      `json:"recipient"`
	MessagingType string         `json:"messaging_type,omitempty"`
	Tag           string         `json:"tag,omitempty"`
	Message       *Message       `json:"message,omitempty"`
	ReplyTo       *ReplyTo       `json:"reply_to,omitempty"`
	SenderAction  string         `json:"sender_action,omitempty"`
	Payload       *ActionPayload `json:"payload,omitempty"`
}

func Text(to Recipient, text string) Envelope {
	return Envelope{Recipient: to, Message: &Message{Text: text}}
}

func Media(to Recipient, attachmentType, url string) Envelope {
	return Envelope{Recipient: to, Message: &Message{Attachment: &Attachment{
		Type:    attachmentType,
		Payload: AttachmentPayload{URL: url},
	}}}
}

func MediaByID(to Recipient, attachmentType, attachmentID string) Envelope {
	return Envelope{Recipient: to, Message: &Message{Attachment: &Attachment{
		Type:    attachmentType,
		Payload: AttachmentPayload{AttachmentID: attachmentID},
	}}}
}

func Buttons(to Recipient, text string, buttons []Button) Envelope {
	return Envelope{Recipient: to, Message: &Message{Attachment: &Attachment{
		Type:    "template",
		Payload: AttachmentPayload{TemplateType: "button", Text: text, Buttons: buttons},
	}}}
}

func Action(to Recipient, action string) Envelope {
	return Envelope{Recipient: to, SenderAction: action}
}

func React(to Recipient, messageID, reaction string) Envelope {
	return Envelope{Recipient: to, SenderAction: ActionReact, Payload: &ActionPayload{MessageID: messageID, Reaction: reaction}}
}

func Unreact(to Recipient, messageID string) Envelope {
	return Envelope{Recipient: to, SenderAction: ActionUnreact, Payload: &ActionPayload{MessageID: messageID}}
}

func (e Envelope) ReplyingTo(mid string) Envelope {
	if strings.TrimSpace(mid) != "" {
		e.ReplyTo = &ReplyTo{MID: mid}
	}
	return e
}

func (e Envelope) As(messagingType, tag string) Envelope {
	e.MessagingType = messagingType
	e.Tag = tag
	return e
}

func (e Envelope) WithMetadata(metadata string) Envelope {
	if e.Message != nil {
		msg := *e.Message
		msg.Metadata = metadata
		e.Message = &msg
	}
	return e
}

func (e Envelope) WithQuickReplies(replies []QuickReply) Envelope {
	if e.Message != nil && len(replies) > 0 {
		msg := *e.Message
		msg.QuickReplies = replies
		e.Message = &msg
	}
	return e
}

func (e Envelope) IsSenderAction() bool { return e.SenderAction != "" }

type Option struct {
	Title   string
	Payload string
}

func QuickReplies(options []Option, maxCount, maxTitleRunes int) []QuickReply {
	if len(options) == 0 {
		return nil
	}
	out := make([]QuickReply, 0, min(len(options), maxCount))
	for _, o := range options {
		if len(out) >= maxCount {
			break
		}
		out = append(out, QuickReply{
			ContentType: "text",
			Title:       TruncateRunes(o.Title, maxTitleRunes),
			Payload:     o.Payload,
		})
	}
	return out
}

func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func AttachmentTypeFor(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "image":
		return "image", nil
	case "video":
		return "video", nil
	case "audio":
		return "audio", nil
	case "document", "file":
		return "file", nil
	}
	return "", fmt.Errorf("sendapi: unsupported media kind %q", kind)
}

type Poster interface {
	Do(ctx context.Context, req meta.Request, out any) error
}

type Response struct {
	RecipientID  string `json:"recipient_id"`
	MessageID    string `json:"message_id"`
	AttachmentID string `json:"attachment_id"`
}

func Send(ctx context.Context, poster Poster, path, token string, env Envelope) (*Response, error) {
	var out Response
	if err := poster.Do(ctx, meta.Request{
		Method:     http.MethodPost,
		Path:       path,
		Token:      token,
		Body:       env,
		Idempotent: env.IsSenderAction(),
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
