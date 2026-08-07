package conversation

import (
	"time"

	"vozko/domain/analysis"
	"vozko/domain/shared"
)

const (
	DefaultInboxPageSize   = 20
	MaxInboxPageSize       = 50
	DefaultHistoryPageSize = 50
	MaxHistoryPageSize     = 100
)

type InboxEntry struct {
	EntryID      string `json:"entry_id"`
	EntryType    string `json:"entry_type"`
	CampaignID   string `json:"campaign_id,omitempty"`
	CampaignName string `json:"campaign_name,omitempty"`
	LeadID       string `json:"lead_id,omitempty"`
	LeadName     string `json:"lead_name,omitempty"`
	LeadNumber   string `json:"lead_number,omitempty"`
	Blocked      bool   `json:"blocked"`
	LeadPicture  string `json:"lead_picture,omitempty"`
	// IsGroup marks a conversation whose other side is a group chat rather than
	// a person.
	//
	// The UI needs this for more than a badge. A group has no number to dial, no
	// lead to open and no single person to attribute the thread to, so the row's
	// affordances have to be suppressed rather than relabelled — and until this
	// existed a group was indistinguishable from a contact in every list the CRM
	// renders.
	IsGroup                 bool                   `json:"is_group,omitempty"`
	LeadMetadata            map[string]interface{} `json:"lead_metadata,omitempty"`
	EntryVariables          []string               `json:"entry_variables,omitempty"`
	UnreadCount             int64                  `json:"unread_count"`
	LastMessagePreview      string                 `json:"last_message_preview,omitempty"`
	LastMessageAt           time.Time              `json:"last_message_at"`
	LastMessageType         string                 `json:"last_message_type,omitempty"`
	LastMessageSender       string                 `json:"last_message_sender,omitempty"`
	LastMessageSenderAvatar string                 `json:"last_message_sender_avatar,omitempty"`
	WindowOpen              bool                   `json:"window_open"`
	WindowExpiresAt         *time.Time             `json:"window_expires_at,omitempty"`
	BusinessPhoneID         string                 `json:"business_phone_id,omitempty"`
	AssignedUserID          string                 `json:"assigned_user_id,omitempty"`
	AssignedUsername        string                 `json:"assigned_username,omitempty"`
	AutomationEnabled       bool                   `json:"automation_enabled"`
	Stage                   *InboxEntryStage       `json:"stage,omitempty"`
	Labels                  []InboxEntryLabel      `json:"labels,omitempty"`
	AvailableStages         []InboxEntryStage      `json:"available_stages,omitempty"`
	MatchedMessages         []MatchedMessage       `json:"matched_messages,omitempty"`
	TotalMatches            int                    `json:"total_matches,omitempty"`
	LatestAnalysis          *analysis.Analysis     `json:"latest_analysis,omitempty"`
	ConversationStatus      ConversationStatus     `json:"conversation_status,omitempty"`
	// Close provenance when status is finished (omitted when open / cleared on reopen).
	CloseSource CloseSource `json:"close_source,omitempty"`
	CloseReason CloseReason `json:"close_reason,omitempty"`
	ClosedAt    *time.Time  `json:"closed_at,omitempty"`
	// AIHandler names the AI attending the conversation (direct agent or workflow) and
	// the live workflow-run state. Nil when no AI is configured on the campaign. The
	// per-conversation on/off state remains AutomationEnabled above.
	AIHandler *AIHandler `json:"ai_handler,omitempty"`
}

// AIHandler describes which AI attends a conversation. Kind is the effective handler
// configured on the campaign ("agent" or "workflow"); a workflow that is currently
// running also carries its live run + current-node state. Whether the AI is paused for
// this specific conversation is the entry's AutomationEnabled flag, not repeated here.
type AIHandler struct {
	Kind string `json:"kind"` // "agent" | "workflow"

	AgentID     string `json:"agent_id,omitempty"`
	AgentName   string `json:"agent_name,omitempty"`
	AgentAvatar string `json:"agent_avatar,omitempty"`
	AgentActive bool   `json:"agent_active"`

	WorkflowID      string `json:"workflow_id,omitempty"`
	WorkflowName    string `json:"workflow_name,omitempty"`
	WorkflowRunID   string `json:"workflow_run_id,omitempty"`
	RunStatus       string `json:"run_status,omitempty"`
	CurrentNodeID   string `json:"current_node_id,omitempty"`
	CurrentNodeType string `json:"current_node_type,omitempty"`
}

type MatchedMessage struct {
	MessageID   string    `json:"message_id"`
	Text        string    `json:"text"`
	From        string    `json:"from"`
	MessageType string    `json:"message_type"`
	Channel     string    `json:"channel"`
	CreatedAt   time.Time `json:"created_at"`
	Position    int64     `json:"position"`
	Page        int       `json:"page"`
}

type InboxEntryStage struct {
	StageID string `json:"stage_id"`
	Name    string `json:"name"`
	Color   string `json:"color,omitempty"`
}

type InboxEntryLabel struct {
	LabelID string `json:"label_id"`
	Name    string `json:"name"`
	Color   string `json:"color,omitempty"`
}

type SearchFilters struct {
	StageID            string `json:"stage_id,omitempty"`
	StageName          string `json:"stage_name,omitempty"`
	MinMessageCount    *int   `json:"min_message_count,omitempty"`
	MaxMessageCount    *int   `json:"max_message_count,omitempty"`
	MessageSearch      string `json:"message_search,omitempty"`
	WindowOpen         *bool  `json:"window_open,omitempty"`
	HasUnread          *bool  `json:"has_unread,omitempty"`
	Channel            string `json:"channel,omitempty"`
	DateFrom           string `json:"date_from,omitempty"`
	DateTo             string `json:"date_to,omitempty"`
	ConversationStatus string `json:"conversation_status,omitempty"`
}

type SearchInboxInput struct {
	UserID               string
	CampaignID           string
	CampaignType         string
	WhatsAppCampaignType string
	WorkspaceID          string
	SelectedDepartmentID string
	DepartmentIDs        []string
	RestrictDepartments  bool

	Query string

	StageID          string
	StageIDs         []string
	StageName        string
	StageWorkspaceID string
	MinMessageCount  *int
	MaxMessageCount  *int
	MessageSearch    string
	WindowOpen       *bool
	HasUnread        *bool
	Channel          string
	DateFrom         *time.Time
	DateTo           *time.Time

	ConversationStatus ConversationStatus

	SortOrder string

	AssignedUserID string

	// ResponsibleUserID / ResponsibleUnassigned are the USER-FACING "filter by
	// responsible" selection, distinct from AssignedUserID (the permission scope, which
	// inbox_service clears for privileged users). These must SURVIVE that clearing.
	ResponsibleUserID     string
	ResponsibleUnassigned bool

	AssigneeOverrideUserID string

	IsAdmin bool

	Page     int
	PageSize int
}

type DepartmentAccessScope struct {
	DepartmentIDs           []string
	Restrict                bool
	WorkspaceHasDepartments bool
}

type StageProvider interface {
	GetEntryStage(entryID, entryType, workspaceID string) (*InboxEntryStage, error)
	GetBatchEntryStages(entryIDs []string, entryType, workspaceID string) (map[string]*InboxEntryStage, error)
	GetEntriesByTagID(StageID, workspaceID string) ([]string, error)
	FindStageIDsByName(workspaceID, name string) ([]string, error)
	GetStageCountsForCampaign(workspaceID, campaignID, entryType string) (map[string]int64, error)
	GetStageCountsForWorkspace(workspaceID, entryType string) (map[string]int64, error)
	GetAvailableStageByCampaigns(workspaceID string, campaignIDs []string) (map[string][]InboxEntryStage, error)
}

type LabelProvider interface {
	GetEntryLabels(entryID, entryType, workspaceID string) ([]InboxEntryLabel, error)
	GetBatchEntryLabels(entryIDs []string, entryType, workspaceID string) (map[string][]InboxEntryLabel, error)
	GetEntriesByLabelID(labelID, workspaceID string) ([]string, error)
	GetAvailableLabels(workspaceID string) ([]InboxEntryLabel, error)
}

type AnalysisProvider interface {
	GetBatchLatestAnalysis(entryIDs []string, entryType string) (map[string]*analysis.Analysis, error)
}

type InitialStageAssigner interface {
	AutoAssignInitialStage(workspaceID, campaignID, campaignType, entryID, entryType string)
}

// FinishOptions stamps who/why when moving to finished.
type FinishOptions struct {
	Source CloseSource
	Reason CloseReason
	// ActorID is the user (source=human) or agent (source=ai) that closed the
	// conversation. Recorded on the timeline event so "who finalized this?" is
	// answerable. Empty for system closes.
	ActorID string
}

type ConversationStatusUpdater interface {
	GetConversationStatus(entryID, entryType string) ConversationStatus

	// SetConversationStatus applies a non-finish transition (e.g. ongoing) or
	// finishes with empty FinishOptions treated as human/manual when status is finished.
	// Prefer Finish for explicit provenance.
	SetConversationStatus(entryID, entryType string, status ConversationStatus) error

	// Finish moves to finished with required close_source / close_reason.
	Finish(entryID, entryType string, opts FinishOptions) error

	TransitionOnMessage(entryID, entryType string, msgType MessageType, direction MessageHistoryDirection) error

	GetStatusCounts(workspaceID, campaignID, entryType string) (map[string]int64, error)
}

type ConversationAuthorizer interface {
	CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool
	CanAccessCampaign(userID, workspaceID, campaignID, campaignType string, isAdmin bool) bool
	GetAccessibleEntryIDs(workspaceID, entryType string, isAdmin bool) []string
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (DepartmentAccessScope, bool)

	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool

	IsWorkspaceMember(userID, workspaceID string) bool

	IsWorkspaceOwnerOrAdmin(userID, workspaceID string) bool
}

type CampaignWorkspaceResolver interface {
	GetCampaignWorkspaceID(campaignID, campaignType string) (string, error)

	GetCampaignDepartmentID(campaignID, campaignType string) (string, error)

	GetEntryWorkspaceID(entryID, entryType string) (string, error)
	GetEntryDepartmentID(entryID, entryType string) (string, error)
	GetEntryCampaignID(entryID, entryType string) (string, error)
}

type SendButtonInput struct {
	HeaderType string
	HeaderText string
	BodyText   string
	FooterText string
	Buttons    []ButtonPayload
}

type ButtonPayload struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type MessageSender interface {
	SendTextMessage(entryID, entryType, text, userID, replyToMessageID string) (*Message, error)
	SendMediaMessage(entryID, entryType, mediaID, mediaType, userID, replyToMessageID string, caption string) (*Message, error)
	SendButtonMessage(entryID, entryType, userID, replyToMessageID string, input SendButtonInput) (*Message, error)
}

type HistoryProvider interface {
	GetHistory(entryID string, entryType shared.EntryType, limit int) ([]*Message, bool, int64, error)
	GetHistoryBefore(entryID string, entryType shared.EntryType, before time.Time, limit int) ([]*Message, bool, error)
	GetHistoryAround(entryID string, entryType shared.EntryType, around time.Time, limit int) ([]*Message, bool, bool, int64, error)
	GetUnreadCount(entryID string, entryType shared.EntryType) (int64, error)
	GetEntryInfo(entryID, entryType string) (leadName, leadNumber, leadPicture string, leadMetadata map[string]interface{}, entryVariables []string, automationEnabled bool, err error)
	// ResolveSenderIdentity fills SenderName/SenderAvatar on a single message.
	//
	// Reading a page of history resolves the sender; a message that arrives
	// while the conversation is already open did not, so the two paths
	// disagreed and a reload "fixed" the label. It belongs on the provider
	// because resolving a sender is a lookup across leads, contacts, agents and
	// users, the provider already owns all four.
	ResolveSenderIdentity(entryID, entryType string, message *Message)
	GetWindowStatusForEntry(entryID, entryType string) (windowOpen bool, windowExpiresAt *time.Time)
	GetInboxEntries(userID, workspaceID, campaignID, campaignType string, page, pageSize int) ([]InboxEntry, int64, error)
	GetInboxEntry(entryID, entryType string) (*InboxEntry, error)
	SearchInboxEntries(input SearchInboxInput) ([]InboxEntry, int64, error)
}

type MessageMarker interface {
	MarkAsRead(entryID string, entryType shared.EntryType, messageIDs []string, userID string) error
	SendTypingIndicator(entryID string, entryType shared.EntryType) error
}

type TemplateSender interface {
	SendTemplate(entryID, entryType, templateID string, parameters []string, userID string, workspaceID string) (messageID string, err error)
}

type EventBroadcaster interface {
	BroadcastNewMessage(entryID, entryType string, message *Message)
	BroadcastMessageSent(userID, requestID, entryID, entryType string, message *Message)
	BroadcastMessageError(userID, requestID, entryID, entryType, errorMsg, errorCode string)
	BroadcastRead(entryID, entryType string, messageIDs []string, readBy string, readAt time.Time)
	BroadcastUnreadCount(entryID, entryType string, count int64)
	BroadcastTyping(entryID, entryType, fromUserID string, isTyping bool)
	BroadcastStageUpdate(workspaceID, entryID, entryType string)
	BroadcastLabelUpdate(workspaceID, entryID, entryType string)
	BroadcastEntryUpdate(entryID, entryType string, message *Message)
	BroadcastMessageStatus(entryID, entryType, messageID string, status DeliveryStatus)
	BroadcastAnalysisUpdate(entryID, entryType string, analysis interface{})
}

type InboxService interface {
	GetInboxPage(userID, campaignID, campaignType, campaignWorkspaceID string, page, pageSize int, isAdmin bool) ([]InboxEntry, int64, error)
	GetInboxPageByWorkspace(userID, workspaceID, entryTypeFilter, whatsappCampaignType string, page, pageSize int, isAdmin bool) ([]InboxEntry, int64, error)
	SearchInbox(userID string, input SearchInboxInput) ([]InboxEntry, int64, error)
	BuildInboxEntry(entryID, entryType string) (*InboxEntry, error)
	SendTemplateForEntry(entryID, entryType, templateID string, parameters []string, userID, workspaceID string, isAdmin bool) (string, error)
	GetInboxStageCounts(workspaceID, campaignID, entryType string) (map[string]int64, error)
	GetConversationStatusCounts(workspaceID, campaignID, entryType string) (map[string]int64, error)
	GetAvailableLabels(workspaceID string) ([]InboxEntryLabel, error)
}

type EligibleUserProvider interface {
	GetEligibleUsersForWorkspace(workspaceID string, skipAdmins bool) []string
	GetEligibleUsersForWorkspaceDepartment(workspaceID, departmentID string, skipAdmins bool) []string
}
