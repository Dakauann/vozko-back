package webchat

import (
	"time"
	"unicode/utf8"
)

const (
	MaxPendingOptions   = 10
	MaxOptionTitleRunes = 80
	MaxPageURLBytes     = 2048
)

type Option struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type Conversation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	WidgetID    string `json:"widgetId"`
	VisitorID   string `json:"visitorId"`

	ConversationStatus string     `json:"conversationStatus,omitempty"`
	CloseSource        string     `json:"closeSource,omitempty"`
	CloseReason        string     `json:"closeReason,omitempty"`
	ClosedAt           *time.Time `json:"closedAt,omitempty"`
	AutomationEnabled  *bool      `json:"automationEnabled,omitempty"`

	PendingOptions []Option `json:"pendingOptions,omitempty"`
	PageURL        string   `json:"pageUrl,omitempty"`

	LastMessageAt         *time.Time `json:"lastMessageAt,omitempty"`
	LastCustomerMessageAt *time.Time `json:"lastCustomerMessageAt,omitempty"`
	LastAgentMessageAt    *time.Time `json:"lastAgentMessageAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c *Conversation) OfferedOption(id string) (Option, bool) {
	if id == "" {
		return Option{}, false
	}
	for _, o := range c.PendingOptions {
		if o.ID == id {
			return o, true
		}
	}
	return Option{}, false
}

func BoundOptions(options []Option) []Option {
	out := make([]Option, 0, len(options))
	for _, o := range options {
		if o.ID == "" || o.Title == "" || len(out) == MaxPendingOptions {
			continue
		}
		if utf8.RuneCountInString(o.Title) > MaxOptionTitleRunes {
			o.Title = string([]rune(o.Title)[:MaxOptionTitleRunes])
		}
		out = append(out, o)
	}
	return out
}

type FindOrCreateConversationInput struct {
	WorkspaceID string
	WidgetID    string
	VisitorID   string
	PageURL     string
}
