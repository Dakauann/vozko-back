package webchat

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"vozko/domain/conversation"
)

const (
	MaxVisitorTextRunes   = 2000
	minClientMessageIDLen = 8
	maxClientMessageIDLen = 64
	inboundIDPrefix       = "wc_in_"
	outboundIDPrefix      = "wc_out_"
)

type Author string

const (
	AuthorVisitor   Author = "visitor"
	AuthorTeam      Author = "team"
	AuthorAssistant Author = "assistant"
)

type VisitorMedia struct {
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	MimeType string `json:"mimeType,omitempty"`
	Filename string `json:"filename,omitempty"`
}

type VisitorMessage struct {
	ID        string        `json:"id"`
	Author    Author        `json:"author"`
	Text      string        `json:"text,omitempty"`
	Media     *VisitorMedia `json:"media,omitempty"`
	Options   []Option      `json:"options,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
}

var visibleMessageTypes = map[conversation.MessageType]struct{}{
	conversation.MessageTypeUserMessage: {},
	conversation.MessageTypeOperator:    {},
	conversation.MessageTypeAIResponse:  {},
	conversation.MessageTypeMedia:       {},
	conversation.MessageTypeAudio:       {},
	conversation.MessageTypeSticker:     {},
}

func ProjectMessage(m *conversation.Message) (VisitorMessage, bool) {
	if m == nil {
		return VisitorMessage{}, false
	}
	if _, ok := visibleMessageTypes[m.MessageType]; !ok {
		return VisitorMessage{}, false
	}
	out := VisitorMessage{
		ID:        m.ID,
		Author:    authorOf(m),
		Text:      m.Text,
		CreatedAt: m.CreatedAt,
	}
	if m.ExternalMessageID != nil && *m.ExternalMessageID != "" {
		out.ID = *m.ExternalMessageID
	}
	if m.Media != nil && m.Media.URL != "" {
		out.Media = &VisitorMedia{Kind: mediaKind(m), URL: m.Media.URL, MimeType: m.Media.MimeType, Filename: m.Media.Filename}
	}
	if out.Text == "" && out.Media == nil {
		return VisitorMessage{}, false
	}
	return out, true
}

func authorOf(m *conversation.Message) Author {
	switch m.SentBy.Kind() {
	case conversation.SenderContact:
		return AuthorVisitor
	case conversation.SenderHuman, conversation.SenderExternal:
		return AuthorTeam
	case conversation.SenderAI, conversation.SenderWorkflow, conversation.SenderCampaign, conversation.SenderSystem:
		return AuthorAssistant
	}
	switch {
	case m.MessageType == conversation.MessageTypeOperator:
		return AuthorTeam
	case m.ResolvedDirection() == conversation.MessageDirectionInbound:
		return AuthorVisitor
	}
	return AuthorAssistant
}

func mediaKind(m *conversation.Message) string {
	if m.MediaType != "" {
		return string(m.MediaType)
	}
	return string(conversation.MediaTypeDocument)
}

func CleanVisitorText(raw string) (string, error) {
	var b strings.Builder
	for _, r := range raw {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			continue
		}
		b.WriteRune(r)
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return "", ErrMessageEmpty
	}
	if utf8.RuneCountInString(text) > MaxVisitorTextRunes {
		return "", ErrMessageTooLong
	}
	return text, nil
}

func InboundProviderID(visitorID, clientMessageID string) (string, error) {
	if len(clientMessageID) < minClientMessageIDLen || len(clientMessageID) > maxClientMessageIDLen {
		return "", ErrClientMessageIDInvalid
	}
	for _, r := range clientMessageID {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return "", ErrClientMessageIDInvalid
		}
	}
	return inboundIDPrefix + visitorID + "_" + clientMessageID, nil
}

func NewOutboundProviderID() string {
	return outboundIDPrefix + uuid.NewString()
}
