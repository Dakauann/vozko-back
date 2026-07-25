package support_inbox

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInboxNotFound           = errors.New("support inbox not found")
	ErrInboxNameRequired       = errors.New("support inbox name is required")
	ErrInboxOriginInvalid      = errors.New("allowed origin is invalid")
	ErrInboxOriginRequired     = errors.New("at least one allowed origin is required for the embeddable widget")
	ErrInboxAlreadyArchived    = errors.New("support inbox is already archived")
	ErrPreChatFieldNameEmpty   = errors.New("pre-chat field name is required")
	ErrPreChatFieldTypeInvalid = errors.New("pre-chat field type must be text, email, textarea, or select")
)

type PreChatField struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}

var validPreChatFieldTypes = map[string]bool{
	"text":     true,
	"email":    true,
	"textarea": true,
	"select":   true,
}

func (f *PreChatField) Validate() error {
	if strings.TrimSpace(f.Name) == "" {
		return ErrPreChatFieldNameEmpty
	}
	if !validPreChatFieldTypes[f.Type] {
		return ErrPreChatFieldTypeInvalid
	}
	return nil
}

type SupportInbox struct {
	ID                   string         `json:"id"`
	WorkspaceID          string         `json:"workspaceId"`
	Name                 string         `json:"name"`
	AgentID              string         `json:"agentId,omitempty"`
	EnableAgentResponses bool           `json:"enableAgentResponses"`
	GreetingMessage      string         `json:"greetingMessage,omitempty"`
	WidgetColor          string         `json:"widgetColor,omitempty"`
	AllowedOrigins       []string       `json:"allowedOrigins"`
	PreChatFields        []PreChatField `json:"preChatFields,omitempty"`
	Archived             bool           `json:"archived"`
	CreatedAt            time.Time      `json:"createdAt"`
	UpdatedAt            time.Time      `json:"updatedAt"`
}

func (i *SupportInbox) Normalize() {
	i.Name = strings.TrimSpace(i.Name)
	i.AgentID = strings.TrimSpace(i.AgentID)
	i.GreetingMessage = strings.TrimSpace(i.GreetingMessage)
	i.WidgetColor = strings.TrimSpace(i.WidgetColor)

	cleaned := make([]string, 0, len(i.AllowedOrigins))
	for _, o := range i.AllowedOrigins {
		o = strings.TrimSpace(o)
		o = strings.TrimRight(o, "/")
		if o != "" {
			cleaned = append(cleaned, o)
		}
	}
	i.AllowedOrigins = cleaned
}

func (i *SupportInbox) Validate() error {
	if strings.TrimSpace(i.Name) == "" {
		return ErrInboxNameRequired
	}
	for _, o := range i.AllowedOrigins {
		if !isValidOrigin(o) {
			return ErrInboxOriginInvalid
		}
	}
	for idx := range i.PreChatFields {
		if err := i.PreChatFields[idx].Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (i *SupportInbox) IsOriginAllowed(origin string) bool {
	if len(i.AllowedOrigins) == 0 {
		return true
	}
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin == "" {
		return false
	}
	for _, o := range i.AllowedOrigins {
		if o == "*" {
			return true
		}
		if strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}

func isValidOrigin(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return false
	}
	if origin == "*" {
		return true
	}
	return strings.HasPrefix(origin, "http://") || strings.HasPrefix(origin, "https://")
}
