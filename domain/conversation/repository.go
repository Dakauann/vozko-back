package conversation

import (
	"time"

	"vozko/domain/crmfilter"
	"vozko/domain/shared"
)

type MarkAsReadInput struct {
	EntryID       string
	EntryType     shared.EntryType
	ReadBy        string
	UpToTimestamp *time.Time
	MessageIDs    []string
}

type ListMessagesInput struct {
	EntryID   string
	EntryType shared.EntryType
	Limit     int
	Before    *time.Time
	After     *time.Time
}

type UnreadCount struct {
	EntryID   string
	EntryType shared.EntryType
	Count     int64
}

type SearchMessagesByEntryInput struct {
	EntryID   string
	EntryType shared.EntryType
	Query     string
	Page      int
	PageSize  int
}

type SearchEntriesInput struct {
	CampaignID           string
	WorkspaceID          string
	WhatsAppCampaignType string
	DepartmentIDs        []string
	RestrictDepartments  bool
	EntryIDs             []string
	EntryType            shared.EntryType

	Query string

	MessageSearch string

	StageIDs         []string
	StageWorkspaceID string

	MinMessageCount *int
	MaxMessageCount *int

	HasUnread  *bool
	WindowOpen *bool

	Channel  string
	DateFrom *time.Time
	DateTo   *time.Time

	ConversationStatus ConversationStatus

	SortOrder string

	AssignedUserID string

	// User-facing "filter by responsible": owner = ResponsibleUserID, or (when
	// ResponsibleUnassigned) conversations with no responsible at all.
	ResponsibleUserID     string
	ResponsibleUnassigned bool

	AssigneeOverrideUserID string

	Page     int
	PageSize int
}

// SearchByFilterInput is the workspace-global, filter-driven read path that
// backs the decoupled CRM board and list view. Unlike SearchEntriesInput (which
// hard-codes each predicate) it carries a reusable crmfilter.Filter compiled by
// the infra FilterCompiler, so the board, the per-column queries and the flat
// list all share one predicate surface. It is ADDITIVE: the campaign/workspace
// SearchEntriesInput paths are untouched.
type SearchByFilterInput struct {
	WorkspaceID string

	DepartmentIDs       []string
	RestrictDepartments bool
	// AssigneeOverrideUserID keeps a restricted (non-admin) member's own
	// assigned conversations visible even outside their departments, mirroring
	// searchEntriesByWorkspace's department-scope clause.
	AssigneeOverrideUserID string

	// AssignedUserID restricts the result to entries this user owns OR that are
	// unassigned (the shared pool) — the inbox's self-scope for a member who
	// lacks the conversations:view_others permission. Empty means no
	// self-restriction (admins, owners, and members who can view others). Without
	// it the board/list leaked every member's entries. Mirrors SearchInboxInput.
	AssignedUserID string

	Filter crmfilter.Filter

	// SortField: "last_activity" (default) or "created". Value sort is N/A for a
	// conversation (no monetary value column).
	SortField string
	SortOrder string // "asc" | "desc" (default desc)

	Page     int
	PageSize int
}

type MatchedMessageResult struct {
	MessageID    string
	EntryID      string
	Text         string
	From         string
	MsgType      MessageType
	Channel      MessageChannel
	CreatedAt    time.Time
	Position     int64
	TotalMatches int
}

type EntryWithLastMessage struct {
	EntryID         string
	EntryType       shared.EntryType
	CampaignID      string
	CampaignName    string
	LeadID          string
	LeadName        string
	LeadNumber      string
	BusinessPhoneID string
	UnreadCount     int64
	LastMessageText string
	LastMessageType MessageType
	LastMessageAt   time.Time
	LastMessageFrom string
	HasMedia        bool
	MediaType       MediaType
	MatchedMessages []MatchedMessageResult
	TotalMatches    int
	// AssignedUserID is the entry's current inbox owner (from inbox_assignment),
	// hydrated by the CRM board read model so the list/board cards can show a
	// responsável. Empty when unassigned. Not populated by the legacy inbox paths.
	AssignedUserID string
	// Campaign AI configuration, read from the already-joined campaign row so the
	// inbox can show which AI (agent or workflow) attends the conversation. These are
	// the raw config values; the effective handler + live run are resolved in the use
	// case enrichment.
	AgentID               string
	WorkflowID            string
	AgentResponsesEnabled bool
	WorkflowEnabled       bool
}

type MessageRepository interface {
	Create(message *Message) error
	Update(messageID string, message *Message) error
	Delete(messageID string) error
	GetByID(id string) (*Message, error)

	ListByEntry(entryID string, entryType shared.EntryType) ([]*Message, error)
	ListByEntryPaginated(input ListMessagesInput) ([]*Message, error)
	ListByLeadID(leadID string) ([]*Message, error)

	MarkAsRead(input MarkAsReadInput) (int64, error)
	CountUnreadByEntry(entryID string, entryType shared.EntryType) (int64, error)
	CountUnreadByEntries(entryIDs []string, entryType shared.EntryType) ([]UnreadCount, error)

	DeleteByEntry(entryID string, entryType shared.EntryType) error
	DeleteByCampaignID(campaignID string, entryType shared.EntryType) (int64, error)
	CountByCampaignID(campaignID string, entryType shared.EntryType) (int64, error)

	GetEntriesWithMessages(campaignID string, entryIDs []string, entryType shared.EntryType, page, pageSize int, assignedUserID string) ([]EntryWithLastMessage, int64, error)

	SearchEntriesWithMessages(input SearchEntriesInput) ([]EntryWithLastMessage, int64, error)

	// SearchEntriesByFilter is the additive, workspace-global read path driven by
	// a reusable crmfilter.Filter (compiled to SQL by the infra FilterCompiler).
	// It reuses the searchEntriesByWorkspace CTE + LATERAL last-message shape.
	SearchEntriesByFilter(input SearchByFilterInput) ([]EntryWithLastMessage, int64, error)

	SearchMessagesByEntry(input SearchMessagesByEntryInput) ([]*Message, int64, error)

	GetEntryLastMessage(entryID string, entryType shared.EntryType) (*EntryWithLastMessage, error)

	CountByEntry(entryID string, entryType shared.EntryType) (int64, error)

	GetByWhatsAppMessageID(wamid string) (*Message, error)
	UpdateDeliveryStatus(wamid string, status DeliveryStatus) error

	ClearAll() error
}
