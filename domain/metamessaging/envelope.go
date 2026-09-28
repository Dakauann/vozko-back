package metamessaging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrInvalidWebhookPayload = errors.New("meta webhook: invalid payload")

type Envelope struct {
	Object string   `json:"object"`
	Entry  []*Entry `json:"entry"`
}

type Entry struct {
	ID   string `json:"id"`
	Time int64  `json:"time"`

	Messaging []*MessagingEvent `json:"messaging,omitempty"`
	Standby   []*MessagingEvent `json:"standby,omitempty"`
	Changes   []*Change         `json:"changes,omitempty"`

	Field string          `json:"field,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

type Change struct {
	Field string          `json:"field"`
	Value json.RawMessage `json:"value"`
}

type MessagingEvent struct {
	Sender    Participant `json:"sender"`
	Recipient Participant `json:"recipient"`
	Timestamp int64       `json:"timestamp"`

	Message           *Message           `json:"message,omitempty"`
	Reaction          *Reaction          `json:"reaction,omitempty"`
	Read              *Read              `json:"read,omitempty"`
	Delivery          *Delivery          `json:"delivery,omitempty"`
	Postback          *Postback          `json:"postback,omitempty"`
	Referral          *Referral          `json:"referral,omitempty"`
	MessageEdit       *MessageEdit       `json:"message_edit,omitempty"`
	PassThreadControl *ThreadControl     `json:"pass_thread_control,omitempty"`
	TakeThreadControl *ThreadControl     `json:"take_thread_control,omitempty"`
	PolicyEnforcement *PolicyEnforcement `json:"policy_enforcement,omitempty"`
	Optin             *Optin             `json:"optin,omitempty"`
}

type Participant struct {
	ID string `json:"id"`
}

type Message struct {
	MID           string        `json:"mid"`
	Text          string        `json:"text,omitempty"`
	Attachments   []*Attachment `json:"attachments,omitempty"`
	IsEcho        *bool         `json:"is_echo,omitempty"`
	IsDeleted     *bool         `json:"is_deleted,omitempty"`
	IsSelf        *bool         `json:"is_self,omitempty"`
	IsUnsupported *bool         `json:"is_unsupported,omitempty"`
	AppID         json.Number   `json:"app_id,omitempty"`
	Metadata      string        `json:"metadata,omitempty"`
	QuickReply    *QuickReply   `json:"quick_reply,omitempty"`
	Referral      *Referral     `json:"referral,omitempty"`
	ReplyTo       *ReplyTo      `json:"reply_to,omitempty"`
}

func (m *Message) Echo() bool        { return m != nil && boolVal(m.IsEcho) }
func (m *Message) Deleted() bool     { return m != nil && boolVal(m.IsDeleted) }
func (m *Message) Unsupported() bool { return m != nil && boolVal(m.IsUnsupported) }

type ReplyTo struct {
	MID         string `json:"mid,omitempty"`
	IsSelfReply *bool  `json:"is_self_reply,omitempty"`
	Story       *Story `json:"story,omitempty"`
}

type Story struct {
	ID             string `json:"id"`
	URL            string `json:"url"`
	LinkStickerURL string `json:"link_sticker_url,omitempty"`
}

type Attachment struct {
	Type    string             `json:"type"`
	Payload *AttachmentPayload `json:"payload,omitempty"`
}

type AttachmentPayload struct {
	URL         string      `json:"url,omitempty"`
	Title       string      `json:"title,omitempty"`
	ID          string      `json:"id,omitempty"`
	ReelVideoID string      `json:"reel_video_id,omitempty"`
	StickerID   json.Number `json:"sticker_id,omitempty"`
}

type QuickReply struct {
	Payload string `json:"payload"`
}

type Reaction struct {
	MID      string `json:"mid"`
	Action   string `json:"action"`
	Reaction string `json:"reaction,omitempty"`
	Emoji    string `json:"emoji,omitempty"`
}

type Read struct {
	MID       string `json:"mid,omitempty"`
	Watermark int64  `json:"watermark,omitempty"`
}

func (r *Read) WatermarkTime() time.Time { return UnixTime(r.Watermark) }

type Delivery struct {
	MIDs      []string `json:"mids,omitempty"`
	Watermark int64    `json:"watermark"`
}

func (d *Delivery) WatermarkTime() time.Time { return UnixTime(d.Watermark) }

type Postback struct {
	MID      string    `json:"mid"`
	Title    string    `json:"title"`
	Payload  string    `json:"payload"`
	Referral *Referral `json:"referral,omitempty"`
}

type Referral struct {
	Ref            string          `json:"ref"`
	Source         string          `json:"source"`
	Type           string          `json:"type"`
	AdID           string          `json:"ad_id,omitempty"`
	RefererURI     string          `json:"referer_uri,omitempty"`
	AdsContextData *AdsContextData `json:"ads_context_data,omitempty"`
}

type AdsContextData struct {
	AdTitle   string `json:"ad_title,omitempty"`
	PhotoURL  string `json:"photo_url,omitempty"`
	VideoURL  string `json:"video_url,omitempty"`
	PostID    string `json:"post_id,omitempty"`
	ProductID string `json:"product_id,omitempty"`
	FlowID    string `json:"flow_id,omitempty"`
}

type MessageEdit struct {
	MID     string      `json:"mid"`
	Text    string      `json:"text"`
	NumEdit json.Number `json:"num_edit,omitempty"`
}

type ThreadControl struct {
	PreviousOwnerAppID json.Number `json:"previous_owner_app_id,omitempty"`
	NewOwnerAppID      json.Number `json:"new_owner_app_id,omitempty"`
	Metadata           string      `json:"metadata,omitempty"`
}

type PolicyEnforcement struct {
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

type Optin struct {
	Type    string `json:"type,omitempty"`
	Payload string `json:"payload,omitempty"`
	Status  string `json:"notification_messages_status,omitempty"`
}

type EntryEnvelope struct {
	Object string `json:"object"`
	Entry  *Entry `json:"entry"`
}

func DecodeEnvelope(body []byte) ([]*Envelope, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, ErrInvalidWebhookPayload
	}

	switch trimmed[0] {
	case '[':
		var envs []*Envelope
		if err := json.Unmarshal(trimmed, &envs); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidWebhookPayload, err)
		}
		return envs, nil
	case '{':
		var env Envelope
		if err := json.Unmarshal(trimmed, &env); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidWebhookPayload, err)
		}
		return []*Envelope{&env}, nil
	default:
		return nil, ErrInvalidWebhookPayload
	}
}

func SplitEntries(envelopes []*Envelope) []*EntryEnvelope {
	out := make([]*EntryEnvelope, 0, len(envelopes))
	for _, env := range envelopes {
		if env == nil {
			continue
		}
		for _, e := range env.Entry {
			if e == nil {
				continue
			}
			out = append(out, &EntryEnvelope{Object: env.Object, Entry: e})
		}
	}
	return out
}

func UnixTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	if value < 100_000_000_000 {
		return time.Unix(value, 0).UTC()
	}
	return time.UnixMilli(value).UTC()
}

func boolVal(b *bool) bool { return b != nil && *b }
