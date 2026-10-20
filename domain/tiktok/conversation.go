package tiktok

import (
	"strings"
	"time"

	"vozko/domain/channel"
	"vozko/domain/conversation"
)

const MessagingWindow = 48 * time.Hour

const OutboundBudget = 10

const MaxTextRunes = 6000

const MaxImageBytes = 3 * 1024 * 1024

const ImageCapabilityFreshness = 24 * time.Hour

const (
	MIMEJPEG = "image/jpeg"
	MIMEPNG  = "image/png"
)

const (
	MaxCardTitleRunes  = 40
	MaxCardButtons     = 3
	MaxCardButtonRunes = 20
	MaxCardButtonID    = 40
)

func ChannelCapabilities() channel.Capabilities {
	return channel.Capabilities{
		CanInitiateConversation: false,
		SupportsTemplates:       false,
		SupportsReactions:       false,
		SupportsTypingIndicator: true,
		SupportsReadReceipts:    true,
		SupportsRichText:        false,

		MaxTextRunes: MaxTextRunes,

		OutboundWindow:   MessagingWindow,
		OutboundBudget:   OutboundBudget,
		HumanAgentWindow: false,

		SignatureFormat: "%s:\n%s",

		MediaLimits: map[channel.MediaKind]channel.MediaLimit{
			channel.MediaImage: {MaxBytes: MaxImageBytes, MIMETypes: []string{MIMEJPEG, MIMEPNG}},
		},

		Interactive: channel.InteractiveLimits{
			MaxOptionsButtons: MaxCardButtons,
			MaxLabelRunes:     MaxCardButtonRunes,
			MaxPayloadBytes:   MaxCardButtonID,
		},
	}
}

type ConversationType string

const (
	ConversationUnknown  ConversationType = ""
	ConversationStranger ConversationType = "STRANGER"
	ConversationSingle   ConversationType = "SINGLE"
)

func ParseConversationType(raw string) ConversationType {
	switch ConversationType(strings.ToUpper(strings.TrimSpace(raw))) {
	case ConversationStranger:
		return ConversationStranger
	case ConversationSingle:
		return ConversationSingle
	}
	return ConversationUnknown
}

type Referral struct {
	Source          string `json:"source"`
	AdvertiserID    string `json:"advertiserId,omitempty"`
	AdID            string `json:"adId,omitempty"`
	AdName          string `json:"adName,omitempty"`
	EmbedURL        string `json:"embedUrl,omitempty"`
	Ref             string `json:"ref,omitempty"`
	PrefilledText   string `json:"prefilledText,omitempty"`
	ReferredAtMilli int64  `json:"referredAt,omitempty"`
}

type Conversation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	AccountID   string `json:"accountId"`
	ContactID   string `json:"contactId"`

	ConversationID   string           `json:"conversationId"`
	ConversationType ConversationType `json:"conversationType,omitempty"`

	OutboundSinceInbound int        `json:"outboundSinceInbound"`
	BudgetClosedAt       *time.Time `json:"budgetClosedAt,omitempty"`

	ImageSendAllowed   *bool      `json:"imageSendAllowed,omitempty"`
	ImageSendCheckedAt *time.Time `json:"imageSendCheckedAt,omitempty"`

	Referral *Referral `json:"referral,omitempty"`

	ConversationStatus string     `json:"conversationStatus,omitempty"`
	AutomationEnabled  *bool      `json:"automationEnabled,omitempty"`
	ClosedAt           *time.Time `json:"closedAt,omitempty"`

	LastMessageAt         *time.Time `json:"lastMessageAt,omitempty"`
	LastCustomerMessageAt *time.Time `json:"lastCustomerMessageAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c *Conversation) Window(now time.Time) conversation.WindowState {
	if strings.TrimSpace(c.ConversationID) == "" {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable)
	}
	if c.closedByTikTok() {
		return conversation.ClosedWindow(conversation.WindowReasonBudgetExhausted)
	}
	return conversation.EvaluateWindow(ChannelCapabilities(), c.LastCustomerMessageAt, c.OutboundSinceInbound, now, false)
}

func (c *Conversation) closedByTikTok() bool {
	if c.BudgetClosedAt == nil {
		return false
	}
	return c.LastCustomerMessageAt == nil || !c.LastCustomerMessageAt.After(*c.BudgetClosedAt)
}

func (c *Conversation) ImageSendKnown(now time.Time) (allowed bool, known bool) {
	if c.ImageSendAllowed == nil || c.ImageSendCheckedAt == nil {
		return false, false
	}
	if now.Sub(*c.ImageSendCheckedAt) > ImageCapabilityFreshness {
		return false, false
	}
	return *c.ImageSendAllowed, true
}
