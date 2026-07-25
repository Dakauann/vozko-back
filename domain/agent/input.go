package agent

type CreateAgentInput struct {
	WorkspaceID        string            `json:"-"`
	Name               string            `json:"name" req:"true"`
	Description        string            `json:"description"`
	InitialMessage     string            `json:"initialMessage"`
	UseInitialMessage  *bool             `json:"useInitialMessage"`
	MessagingPrompt    string            `json:"messagingPrompt" req:"true"`
	MessagingModel     string            `json:"messagingModel" req:"true"`
	AvatarURL          string            `json:"avatarUrl"`
	Provider           AgentProvider     `json:"provider" req:"true"`
	IsActive           *bool             `json:"isActive"`
	BusinessPhoneID    string            `json:"businessPhoneId"`
	Tags               []string          `json:"-"`
	Metadata           map[string]string `json:"-"`
	RAGEnabled         *bool             `json:"-"`
	RAGConfig          *RAGConfig        `json:"-"`
	InternalTools      []ToolBinding     `json:"-"`
	WhatsAppTemplateID *string           `json:"-"`
	MediaIDs           []string          `json:"-"`
	KnowledgeBaseIDs   []string          `json:"-"`
	MCPCollectionIDs   []string          `json:"-"`
	Variables          []AgentVariable   `json:"-"`
}

func BuildForCreate(in CreateAgentInput) *Agent {
	return &Agent{
		WorkspaceID:        in.WorkspaceID,
		Name:               in.Name,
		Description:        in.Description,
		InitialMessage:     in.InitialMessage,
		UseInitialMessage:  boolOr(in.UseInitialMessage, true),
		MessagingPrompt:    in.MessagingPrompt,
		MessagingModel:     in.MessagingModel,
		AvatarURL:          in.AvatarURL,
		Tags:               in.Tags,
		Metadata:           in.Metadata,
		Provider:           in.Provider,
		IsActive:           boolOr(in.IsActive, true),
		RAGEnabled:         boolOr(in.RAGEnabled, false),
		RAGConfig:          in.RAGConfig,
		InternalTools:      in.InternalTools,
		BusinessPhoneID:    in.BusinessPhoneID,
		WhatsAppTemplateID: in.WhatsAppTemplateID,
		MediaIDs:           in.MediaIDs,
		KnowledgeBaseIDs:   in.KnowledgeBaseIDs,
		MCPCollectionIDs:   in.MCPCollectionIDs,
		Variables:          in.Variables,
	}
}

type UpdateAgentInput struct {
	Name              *string           `json:"name"`
	Description       *string           `json:"description"`
	InitialMessage    *string           `json:"initialMessage"`
	UseInitialMessage *bool             `json:"useInitialMessage"`
	MessagingPrompt   *string           `json:"messagingPrompt"`
	MessagingModel    *string           `json:"messagingModel"`
	AvatarURL         *string           `json:"avatarUrl"`
	Provider          *AgentProvider    `json:"provider"`
	BusinessPhoneID   *string           `json:"businessPhoneId"`
	IsActive          *bool             `json:"isActive"`
	RAGEnabled        *bool             `json:"-"`
	Archived          *bool             `json:"-"`
	Tags              []string          `json:"-"`
	Metadata          map[string]string `json:"-"`
	InternalTools     []ToolBinding     `json:"-"`
	MediaIDs          []string          `json:"-"`
	KnowledgeBaseIDs  []string          `json:"-"`
	MCPCollectionIDs  []string          `json:"-"`
	Variables         []AgentVariable   `json:"-"`

	ReplaceWhatsAppTemplate bool       `json:"-"`
	WhatsAppTemplateID      *string    `json:"-"`
	ReplaceRAGConfig        bool       `json:"-"`
	RAGConfig               *RAGConfig `json:"-"`
}

func (a *Agent) ApplyUpdate(in UpdateAgentInput) {
	if in.Name != nil {
		a.Name = *in.Name
	}
	if in.Description != nil {
		a.Description = *in.Description
	}
	if in.InitialMessage != nil {
		a.InitialMessage = *in.InitialMessage
	}
	if in.UseInitialMessage != nil {
		a.UseInitialMessage = *in.UseInitialMessage
	}
	if in.MessagingPrompt != nil {
		a.MessagingPrompt = *in.MessagingPrompt
	}
	if in.MessagingModel != nil {
		a.MessagingModel = *in.MessagingModel
	}
	if in.AvatarURL != nil {
		a.AvatarURL = *in.AvatarURL
	}
	if in.Provider != nil {
		a.Provider = *in.Provider
	}
	if in.BusinessPhoneID != nil {
		a.BusinessPhoneID = *in.BusinessPhoneID
	}
	if in.IsActive != nil {
		a.IsActive = *in.IsActive
	}
	if in.RAGEnabled != nil {
		a.RAGEnabled = *in.RAGEnabled
	}
	if in.Archived != nil {
		a.Archived = *in.Archived
	}
	if in.Tags != nil {
		a.Tags = in.Tags
	}
	if in.Metadata != nil {
		a.Metadata = in.Metadata
	}
	if in.InternalTools != nil {
		a.InternalTools = in.InternalTools
	}
	if in.MediaIDs != nil {
		a.MediaIDs = in.MediaIDs
	}
	if in.KnowledgeBaseIDs != nil {
		a.KnowledgeBaseIDs = in.KnowledgeBaseIDs
	}
	if in.MCPCollectionIDs != nil {
		a.MCPCollectionIDs = in.MCPCollectionIDs
	}
	if in.Variables != nil {
		a.Variables = in.Variables
	}
	if in.ReplaceWhatsAppTemplate {
		a.WhatsAppTemplateID = in.WhatsAppTemplateID
	}
	if in.ReplaceRAGConfig {
		a.RAGConfig = in.RAGConfig
	}
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}
