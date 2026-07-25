package agent

import (
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxAgentVariables = 50

var variableNamePattern = regexp.MustCompile(`^\w+$`)

const (
	ToolSendWhatsAppMessage = "send_whatsapp_message"
)

var (
	ErrAgentNameRequired                    = errors.New("agent name is required")
	ErrAgentNameTooLong                     = errors.New("agent name must not exceed 120 characters")
	ErrAgentInitialMessageRequired          = errors.New("agent initial message is required")
	ErrAgentMessagingPromptRequired         = errors.New("agent messaging prompt is required")
	ErrAgentMessagingModelRequired          = errors.New("agent messaging model is required")
	ErrAgentMessagingModelTooLong           = errors.New("agent messaging model must not exceed 64 characters")
	ErrAgentProviderRequired                = errors.New("agent provider is required")
	ErrAgentProviderInvalid                 = errors.New("agent provider is invalid")
	ErrAgentProviderUnsupported             = errors.New("agent provider is unsupported")
	ErrAgentProviderUnavailable             = errors.New("agent provider is unavailable")
	ErrAgentBusinessPhoneNotFound           = errors.New("agent business phone not found")
	ErrAgentBusinessPhoneNoAccess           = errors.New("user does not have access to this business phone number")
	ErrAgentNotFound                        = errors.New("agent not found")
	ErrAgentMetadataInvalid                 = errors.New("agent metadata must be valid JSON object")
	ErrAgentInternalToolInvalid             = errors.New("agent internal tool is invalid")
	ErrAgentInternalToolsRequired           = errors.New("agent internal tools are required")
	ErrAgentWhatsAppTemplateRequiredForTool = errors.New("whatsappTemplateId is required when using send_whatsapp_message tool")
	ErrAgentToolVisibilityInvalid           = errors.New("agent tool visibility is invalid")
	ErrAgentVariableNameInvalid             = errors.New("agent variable names must contain only letters, digits, and underscores")
	ErrAgentVariableDuplicate               = errors.New("agent variable names must be unique")
	ErrAgentVariableTooMany                 = errors.New("agent must not have more than 50 variables")
	ErrAgentRequiredVariableMissing         = errors.New("required agent variable is missing from entry metadata")
)

type ToolVisibility string

const (
	ToolVisibilityMessaging ToolVisibility = "messaging"
)

func (v ToolVisibility) IsValid() bool {
	return v == ToolVisibilityMessaging
}

type ToolBinding struct {
	Name       string                 `json:"name"`
	Visibility []ToolVisibility       `json:"visibility,omitempty"`
	Config     map[string]interface{} `json:"config,omitempty"`
}

type AgentVariable struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	DefaultValue string `json:"defaultValue,omitempty"`
}

func (tb ToolBinding) IsVisibleIn(v ToolVisibility) bool {
	if len(tb.Visibility) == 0 {
		return true
	}
	for _, vis := range tb.Visibility {
		if vis == v {
			return true
		}
	}
	return false
}

func (tb ToolBinding) GetConfig(key string) (interface{}, bool) {
	if tb.Config == nil {
		return nil, false
	}
	val, ok := tb.Config[key]
	return val, ok
}

func (tb ToolBinding) GetConfigString(key string) string {
	if val, ok := tb.GetConfig(key); ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func (tb ToolBinding) GetConfigMap(key string) map[string]interface{} {
	if val, ok := tb.GetConfig(key); ok {
		if m, ok := val.(map[string]interface{}); ok {
			return m
		}
	}
	return nil
}

type AgentProvider string

const (
	// AgentProviderAI is the platform-managed AI voice provider. The value is
	// brand-neutral: it is stored in the DB, returned by the API, and sent by
	// clients. Existing rows were migrated in infra/database/migrate.go.
	AgentProviderAI AgentProvider = "platform"
)

func (p AgentProvider) IsValid() bool {
	switch p {
	case AgentProviderAI, "internal":
		return true
	default:
		return false
	}
}

type RAGConfig struct {
	MaxCharacters int `json:"maxCharacters,omitempty"`

	MaxChunks int `json:"maxChunks,omitempty"`

	MinScore float64 `json:"minScore,omitempty"`

	NumCandidates int `json:"numCandidates,omitempty"`

	ContextWindow int `json:"contextWindow,omitempty"`

	MaxChunksPerDoc int `json:"maxChunksPerDoc,omitempty"`
}

func (c *RAGConfig) WithDefaults() RAGConfig {
	out := RAGConfig{
		MaxCharacters:   10000,
		MaxChunks:       5,
		MinScore:        0.3,
		NumCandidates:   10,
		ContextWindow:   1,
		MaxChunksPerDoc: 3,
	}
	if c == nil {
		return out
	}
	if c.MaxCharacters > 0 {
		out.MaxCharacters = c.MaxCharacters
	}
	if c.MaxChunks > 0 {
		out.MaxChunks = c.MaxChunks
	}
	if c.MinScore > 0 {
		out.MinScore = c.MinScore
	}
	if c.NumCandidates > 0 {
		out.NumCandidates = c.NumCandidates
	}

	if c.ContextWindow > 0 {
		out.ContextWindow = c.ContextWindow
	}
	if c.MaxChunksPerDoc > 0 {
		out.MaxChunksPerDoc = c.MaxChunksPerDoc
	}
	return out
}

type Agent struct {
	ID               string   `json:"id"`
	WorkspaceID      string   `json:"workspaceId"`
	DepartmentID     string   `json:"departmentId,omitempty"`
	KnowledgeBaseIDs []string `json:"knowledgeBaseIds,omitempty"`
	MCPCollectionIDs []string `json:"mcpCollectionIds,omitempty"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	InitialMessage   string   `json:"initialMessage"`

	UseInitialMessage  bool              `json:"useInitialMessage"`
	MessagingPrompt    string            `json:"messagingPrompt"`
	MessagingModel     string            `json:"messagingModel"`
	AvatarURL          string            `json:"avatarUrl"`
	Tags               []string          `json:"tags"`
	Metadata           map[string]string `json:"metadata"`
	InternalTools      []ToolBinding     `json:"internalTools"`
	ToolBindings       map[string]string `json:"toolBindings"`
	Provider           AgentProvider     `json:"provider"`
	ExternalID         string            `json:"externalId"`
	BusinessPhoneID    string            `json:"businessPhoneId,omitempty"`
	WhatsAppTemplateID *string           `json:"whatsappTemplateId"`
	RAGEnabled         bool              `json:"ragEnabled"`
	RAGConfig          *RAGConfig        `json:"ragConfig,omitempty"`
	IsActive           bool              `json:"isActive"`
	Archived           bool              `json:"archived"`
	CreatedAt          time.Time         `json:"createdAt"`
	UpdatedAt          time.Time         `json:"updatedAt"`
	MediaIDs           []string          `json:"mediaIds"`
	Variables          []AgentVariable   `json:"variables,omitempty"`
}

func (a *Agent) Normalize() {
	a.DepartmentID = strings.TrimSpace(a.DepartmentID)
	a.Name = strings.TrimSpace(a.Name)
	a.Description = strings.TrimSpace(a.Description)
	a.InitialMessage = strings.TrimSpace(a.InitialMessage)
	a.MessagingPrompt = strings.TrimSpace(a.MessagingPrompt)
	a.MessagingModel = strings.TrimSpace(a.MessagingModel)
	a.AvatarURL = strings.TrimSpace(a.AvatarURL)
	a.ExternalID = strings.TrimSpace(a.ExternalID)
	a.BusinessPhoneID = strings.TrimSpace(a.BusinessPhoneID)

	prov := strings.TrimSpace(strings.ToLower(string(a.Provider)))
	a.Provider = AgentProvider(prov)

	if len(a.Tags) > 0 {
		seen := make(map[string]struct{}, len(a.Tags))
		uniq := make([]string, 0, len(a.Tags))
		for _, tag := range a.Tags {
			t := strings.TrimSpace(strings.ToLower(tag))
			if t == "" {
				continue
			}
			if _, exists := seen[t]; exists {
				continue
			}
			seen[t] = struct{}{}
			uniq = append(uniq, t)
		}
		a.Tags = uniq
	}

	if len(a.InternalTools) > 0 {
		seen := make(map[string]struct{}, len(a.InternalTools))
		uniq := make([]ToolBinding, 0, len(a.InternalTools))
		for _, binding := range a.InternalTools {
			t := strings.TrimSpace(strings.ToLower(binding.Name))
			if t == "" {
				continue
			}
			if _, exists := seen[t]; exists {
				continue
			}
			seen[t] = struct{}{}

			normalizedVisibility := make([]ToolVisibility, 0, len(binding.Visibility))
			for _, v := range binding.Visibility {
				vNorm := ToolVisibility(strings.TrimSpace(strings.ToLower(string(v))))
				if vNorm.IsValid() {
					normalizedVisibility = append(normalizedVisibility, vNorm)
				}
			}
			uniq = append(uniq, ToolBinding{
				Name:       t,
				Visibility: normalizedVisibility,
				Config:     binding.Config,
			})
		}
		a.InternalTools = uniq
	} else if a.InternalTools != nil && len(a.InternalTools) == 0 {
		a.InternalTools = []ToolBinding{}
	}

	if a.Metadata == nil {
		a.Metadata = make(map[string]string)
	}
	if a.ToolBindings == nil {
		a.ToolBindings = make(map[string]string)
	}

	if len(a.Variables) > 0 {
		seen := make(map[string]struct{}, len(a.Variables))
		uniq := make([]AgentVariable, 0, len(a.Variables))
		for _, v := range a.Variables {
			name := strings.TrimSpace(v.Name)
			if name == "" {
				continue
			}
			lower := strings.ToLower(name)
			if _, exists := seen[lower]; exists {
				continue
			}
			seen[lower] = struct{}{}
			uniq = append(uniq, AgentVariable{
				Name:         name,
				Description:  strings.TrimSpace(v.Description),
				DefaultValue: strings.TrimSpace(v.DefaultValue),
			})
		}
		a.Variables = uniq
	}
}

func (a *Agent) Validate() error {
	if a.Name == "" {
		return ErrAgentNameRequired
	}
	if len(a.Name) > 120 {
		return ErrAgentNameTooLong
	}

	if a.UseInitialMessage && a.InitialMessage == "" {
		return ErrAgentInitialMessageRequired
	}
	if a.MessagingPrompt == "" {
		return ErrAgentMessagingPromptRequired
	}
	if a.Provider == "" {
		return ErrAgentProviderRequired
	}
	if !a.Provider.IsValid() {
		return ErrAgentProviderInvalid
	}
	if a.MessagingModel == "" {
		return ErrAgentMessagingModelRequired
	}
	if len(a.MessagingModel) > 64 {
		return ErrAgentMessagingModelTooLong
	}
	if a.Metadata != nil {
		if _, err := json.Marshal(a.Metadata); err != nil {
			return ErrAgentMetadataInvalid
		}
	}
	if a.InternalTools == nil {
		return ErrAgentInternalToolsRequired
	}
	if len(a.Variables) > maxAgentVariables {
		return ErrAgentVariableTooMany
	}
	for _, v := range a.Variables {
		if !variableNamePattern.MatchString(v.Name) {
			return ErrAgentVariableNameInvalid
		}
	}
	seen := make(map[string]struct{}, len(a.Variables))
	for _, v := range a.Variables {
		lower := strings.ToLower(v.Name)
		if _, exists := seen[lower]; exists {
			return ErrAgentVariableDuplicate
		}
		seen[lower] = struct{}{}
	}
	return nil
}

func (a *Agent) ShouldUseInitialMessage() bool {
	if a == nil {
		return false
	}
	if !a.UseInitialMessage {
		return false
	}
	return strings.TrimSpace(a.InitialMessage) != ""
}

func (a *Agent) ProviderToolIDs() []string {
	if len(a.ToolBindings) == 0 {
		return nil
	}
	ids := make([]string, 0, len(a.ToolBindings))
	for _, id := range a.ToolBindings {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func (a *Agent) SyncInternalToolsFromBindings() {
	if a == nil {
		return
	}
	if len(a.ToolBindings) == 0 {
		a.InternalTools = []ToolBinding{}
		return
	}

	existingVisibility := make(map[string][]ToolVisibility, len(a.InternalTools))
	for _, tb := range a.InternalTools {
		if tb.Name != "" {
			existingVisibility[tb.Name] = tb.Visibility
		}
	}

	names := make([]string, 0, len(a.ToolBindings))
	for name := range a.ToolBindings {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		names = append(names, trimmed)
	}
	sort.Strings(names)
	if len(names) == 0 {
		a.InternalTools = []ToolBinding{}
		return
	}

	tools := make([]ToolBinding, 0, len(names))
	for _, name := range names {
		tb := ToolBinding{Name: name}
		if vis, exists := existingVisibility[name]; exists {
			tb.Visibility = vis
		}
		tools = append(tools, tb)
	}
	a.InternalTools = tools
}

func (a *Agent) HasTool(toolName string) bool {
	if a == nil {
		return false
	}
	for _, tb := range a.InternalTools {
		if tb.Name == toolName {
			return true
		}
	}
	return false
}

func (a *Agent) GetToolBinding(toolName string) (ToolBinding, bool) {
	if a == nil {
		return ToolBinding{}, false
	}
	for _, tb := range a.InternalTools {
		if tb.Name == toolName {
			return tb, true
		}
	}
	return ToolBinding{}, false
}

func (a *Agent) GetToolNames() []string {
	if a == nil || len(a.InternalTools) == 0 {
		return nil
	}
	names := make([]string, 0, len(a.InternalTools))
	for _, tb := range a.InternalTools {
		if tb.Name != "" {
			names = append(names, tb.Name)
		}
	}
	return names
}

func (a *Agent) HasWhatsAppMessageTool() bool {
	return a.HasTool(ToolSendWhatsAppMessage)
}

func (a *Agent) RemoveTool(toolName string) {
	if a == nil {
		return
	}
	filtered := make([]ToolBinding, 0, len(a.InternalTools))
	for _, tb := range a.InternalTools {
		if tb.Name != toolName {
			filtered = append(filtered, tb)
		}
	}
	a.InternalTools = filtered

	if a.ToolBindings != nil {
		delete(a.ToolBindings, toolName)
	}
}

func (a *Agent) ValidateWhatsAppToolRequirements() error {
	if a.HasWhatsAppMessageTool() && (a.WhatsAppTemplateID == nil || *a.WhatsAppTemplateID == "") {
		return ErrAgentWhatsAppTemplateRequiredForTool
	}
	return nil
}

func (a *Agent) EnsureWhatsAppToolConsistency() {
	if a.WhatsAppTemplateID == nil || *a.WhatsAppTemplateID == "" {
		a.RemoveTool(ToolSendWhatsAppMessage)
	}
}

func (a *Agent) HasKnowledgeBases() bool {
	return a != nil && len(a.KnowledgeBaseIDs) > 0
}

func (a *Agent) IsRAGEnabled() bool {
	return a != nil && a.RAGEnabled && len(a.KnowledgeBaseIDs) > 0
}

func (a *Agent) AddKnowledgeBase(kbID string) {
	if a == nil || kbID == "" {
		return
	}
	for _, id := range a.KnowledgeBaseIDs {
		if id == kbID {
			return
		}
	}
	a.KnowledgeBaseIDs = append(a.KnowledgeBaseIDs, kbID)
}

func (a *Agent) RemoveKnowledgeBase(kbID string) {
	if a == nil || kbID == "" {
		return
	}
	filtered := make([]string, 0, len(a.KnowledgeBaseIDs))
	for _, id := range a.KnowledgeBaseIDs {
		if id != kbID {
			filtered = append(filtered, id)
		}
	}
	a.KnowledgeBaseIDs = filtered
}

func (a *Agent) HasKnowledgeBase(kbID string) bool {
	if a == nil {
		return false
	}
	for _, id := range a.KnowledgeBaseIDs {
		if id == kbID {
			return true
		}
	}
	return false
}

func (a *Agent) HasMCPCollections() bool {
	return a != nil && len(a.MCPCollectionIDs) > 0
}

func (a *Agent) HasMCPCollection(id string) bool {
	if a == nil {
		return false
	}
	for _, cid := range a.MCPCollectionIDs {
		if cid == id {
			return true
		}
	}
	return false
}
