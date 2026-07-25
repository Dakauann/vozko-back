package supportinbox

import (
	"time"

	"vozko/delivery/http/response"
	supportinboxdomain "vozko/domain/support_inbox"
)

type SupportInboxRequest struct {
	Name                 string         `json:"name" example:"Atendimento Site"`
	AgentID              string         `json:"agentId,omitempty" example:"agt_a1b2c3"`
	EnableAgentResponses bool           `json:"enableAgentResponses" example:"true"`
	GreetingMessage      string         `json:"greetingMessage,omitempty" example:"Olá! Como podemos ajudar?"`
	WidgetColor          string         `json:"widgetColor,omitempty" example:"#2463eb"`
	AllowedOrigins       []string       `json:"allowedOrigins"`
	PreChatFields        []PreChatField `json:"preChatFields,omitempty"`
}

type PreChatField struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}

type CreateSessionRequest struct {
	InboxID      string `json:"inboxId" example:"inbox_a1b2c3"`
	ContactName  string `json:"contactName,omitempty" example:"Maria Silva"`
	ContactEmail string `json:"contactEmail,omitempty" example:"maria@exemplo.com.br"`
	SourceURL    string `json:"sourceUrl,omitempty" example:"https://meusite.com.br/contato"`
}

type ReconnectSessionRequest struct {
	SessionToken string `json:"sessionToken" example:"a1b2c3d4e5f6.7890abcdef"`
}

type SupportInboxResponse struct {
	ID                   string         `json:"id" example:"inbox_a1b2c3"`
	WorkspaceID          string         `json:"workspaceId" example:"ws_a1b2c3"`
	Name                 string         `json:"name" example:"Atendimento Site"`
	AgentID              string         `json:"agentId,omitempty" example:"agt_a1b2c3"`
	EnableAgentResponses bool           `json:"enableAgentResponses" example:"true"`
	GreetingMessage      string         `json:"greetingMessage,omitempty" example:"Olá! Como podemos ajudar?"`
	WidgetColor          string         `json:"widgetColor,omitempty" example:"#2463eb"`
	AllowedOrigins       []string       `json:"allowedOrigins"`
	PreChatFields        []PreChatField `json:"preChatFields,omitempty"`
	Archived             bool           `json:"archived" example:"false"`
	CreatedAt            time.Time      `json:"createdAt"`
	UpdatedAt            time.Time      `json:"updatedAt"`
}

type SupportInboxListResponse struct {
	Data []SupportInboxResponse  `json:"data"`
	Meta response.PaginationMeta `json:"meta"`
}

type WidgetInfoResponse struct {
	InboxID         string         `json:"inboxId" example:"inbox_a1b2c3"`
	Name            string         `json:"name" example:"Atendimento Site"`
	GreetingMessage string         `json:"greetingMessage,omitempty" example:"Olá! Como podemos ajudar?"`
	WidgetColor     string         `json:"widgetColor,omitempty" example:"#2463eb"`
	PreChatFields   []PreChatField `json:"preChatFields,omitempty"`
}

type CreateSessionResponse struct {
	SessionToken    string         `json:"sessionToken" example:"a1b2c3d4e5f6.7890abcdef"`
	EntryID         string         `json:"entryId" example:"entry_a1b2c3"`
	GreetingMessage string         `json:"greetingMessage,omitempty" example:"Olá! Como podemos ajudar?"`
	PreChatFields   []PreChatField `json:"preChatFields,omitempty"`
	ExpiresAt       time.Time      `json:"expiresAt"`
}

type ReconnectSessionResponse struct {
	EntryID         string    `json:"entryId" example:"entry_a1b2c3"`
	InboxID         string    `json:"inboxId" example:"inbox_a1b2c3"`
	GreetingMessage string    `json:"greetingMessage,omitempty" example:"Olá! Como podemos ajudar?"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

func toSupportInboxResponse(i *supportinboxdomain.SupportInbox) *SupportInboxResponse {
	if i == nil {
		return nil
	}
	return &SupportInboxResponse{
		ID:                   i.ID,
		WorkspaceID:          i.WorkspaceID,
		Name:                 i.Name,
		AgentID:              i.AgentID,
		EnableAgentResponses: i.EnableAgentResponses,
		GreetingMessage:      i.GreetingMessage,
		WidgetColor:          i.WidgetColor,
		AllowedOrigins:       i.AllowedOrigins,
		PreChatFields:        fromDomainPreChatFields(i.PreChatFields),
		Archived:             i.Archived,
		CreatedAt:            i.CreatedAt,
		UpdatedAt:            i.UpdatedAt,
	}
}

func toSupportInboxResponses(items []*supportinboxdomain.SupportInbox) []*SupportInboxResponse {
	if items == nil {
		return nil
	}
	out := make([]*SupportInboxResponse, len(items))
	for idx, it := range items {
		out[idx] = toSupportInboxResponse(it)
	}
	return out
}

func toWidgetInfoResponse(i *supportinboxdomain.SupportInbox) WidgetInfoResponse {
	return WidgetInfoResponse{
		InboxID:         i.ID,
		Name:            i.Name,
		GreetingMessage: i.GreetingMessage,
		WidgetColor:     i.WidgetColor,
		PreChatFields:   fromDomainPreChatFields(i.PreChatFields),
	}
}

func toCreateSessionResponse(o *supportinboxdomain.CreateSessionOutput) CreateSessionResponse {
	return CreateSessionResponse{
		SessionToken:    o.SessionToken,
		EntryID:         o.EntryID,
		GreetingMessage: o.GreetingMessage,
		PreChatFields:   fromDomainPreChatFields(o.PreChatFields),
		ExpiresAt:       o.ExpiresAt,
	}
}

func toDomainPreChatFields(fields []PreChatField) []supportinboxdomain.PreChatField {
	converted := make([]supportinboxdomain.PreChatField, len(fields))
	for idx, field := range fields {
		converted[idx] = supportinboxdomain.PreChatField{
			Name: field.Name, Label: field.Label, Type: field.Type,
			Required: field.Required, Options: field.Options,
		}
	}
	return converted
}

func fromDomainPreChatFields(fields []supportinboxdomain.PreChatField) []PreChatField {
	converted := make([]PreChatField, len(fields))
	for idx, field := range fields {
		converted[idx] = PreChatField{
			Name: field.Name, Label: field.Label, Type: field.Type,
			Required: field.Required, Options: field.Options,
		}
	}
	return converted
}

func toReconnectSessionResponse(o *supportinboxdomain.ReconnectSessionOutput) ReconnectSessionResponse {
	return ReconnectSessionResponse{
		EntryID:         o.EntryID,
		InboxID:         o.InboxID,
		GreetingMessage: o.GreetingMessage,
		ExpiresAt:       o.ExpiresAt,
	}
}
