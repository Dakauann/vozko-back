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

// ContainerKind selects WHICH container a scoped inbox query narrows to.
//
// Most channels have exactly one: a WhatsApp campaign, or the account row. The
// unofficial WhatsApp channel has two genuinely different ones — a conversation
// belongs to a NUMBER forever, while a campaign is one run across many numbers —
// and the CRM has to be able to ask for either.
//
// The zero value is the channel's primary container, so every existing caller
// and every channel that declares only one keeps its exact behaviour.
type ContainerKind string

const (
	ContainerKindAccount  ContainerKind = ""
	ContainerKindCampaign ContainerKind = "campaign"
)

// Valid reports whether this is a kind the system recognises. An unknown value
// must fall back to the primary container rather than matching nothing.
func (k ContainerKind) Valid() bool {
	return k == ContainerKindAccount || k == ContainerKindCampaign
}

type SearchEntriesInput struct {
	// ContainerKind narrows CampaignID to a campaign rather than to the
	// channel's primary container. Empty keeps today's behaviour.
	ContainerKind ContainerKind

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
	// unassigned (the shared pool), the inbox's self-scope for a member who
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
	// AutomationEnabled is the PER-CONVERSATION override an operator flips when
	// taking over, distinct from the container's AgentResponsesEnabled above.
	//
	// nil means no override, so the conversation inherits the container switch.
	// It is read from SQL for every channel: it used to be looked up only for
	// WhatsApp, and every other channel silently reported "enabled" no matter
	// what was stored, so pausing a Telegram or Instagram conversation wrote
	// correctly and then read back as still running.
	AutomationEnabled *bool
	// ConversationStatus is new / ongoing / finished, read from SQL for every
	// channel for exactly the reason above.
	//
	// It used to be looked up only through the WhatsApp entry repository, so on
	// every other channel the inbox row was built with no status at all and
	// rendered as "Nova" over a conversation the database had as ongoing. An
	// operator would reply, the row would rebuild, and the conversation
	// appeared to move backwards.
	//
	// Carried on the row rather than fetched per entry: the status column is
	// already projected by the channel union that produces these rows, so this
	// costs nothing, and a per-entry lookup would be one query per row of the
	// inbox.
	ConversationStatus string
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
	// CountInboundByEntry counts only the messages the CONTACT sent.
	//
	// It answers "is this their first message?", which cannot be derived from a
	// page of recent history: late in a conversation the last few rows are
	// mostly outbound, so a windowed count sees exactly one inbound and reports
	// a first message that is actually the forty-fifth.
	CountInboundByEntry(entryID string, entryType shared.EntryType) (int64, error)

	GetByWhatsAppMessageID(wamid string) (*Message, error)

	// GetByExternalMessageID finds the message an event REFERS to, anywhere in
	// the channel. Edit, delete, reaction and read events name a provider id
	// without saying which conversation holds it, so the lookup is what
	// discovers the entry; it cannot be scoped by one.
	GetByExternalMessageID(entryType shared.EntryType, externalID string) (*Message, error)

	// GetByEntryAndExternalMessageID is the DEDUP lookup, and it is scoped to
	// one entry on purpose.
	//
	// A provider message id is unique per conversation, not per platform. When
	// both ends of a chat are accounts we host — one tenant messaging another —
	// the provider stamps one id and it legitimately arrives twice: outbound on
	// the sender's entry, inbound on the receiver's. Matching on (entry type,
	// id) alone read the second as a replay and dropped it, so the receiving
	// tenant never saw the message at all.
	GetByEntryAndExternalMessageID(entryType shared.EntryType, entryID, externalID string) (*Message, error)
	UpdateDeliveryStatus(wamid string, status DeliveryStatus) error
	// UpdateDeliveryStatusWithReason also records the provider's explanation, so
	// the thread can say why rather than only that.
	UpdateDeliveryStatusWithReason(wamid string, status DeliveryStatus, errorCode int, errorMessage string) error
	// UpdateDeliveryReceipt records everything one status webhook says about a
	// message: the status, the failure reason, and Meta's own pricing verdict.
	//
	// The other two are kept because forty-odd callers pass only a status, and
	// both now delegate here so there is one update path rather than three. One
	// path also means one write per receipt, which matters on what is the
	// busiest webhook the platform handles.
	UpdateDeliveryReceipt(wamid string, receipt DeliveryReceipt) error

	ClearAll() error
}
