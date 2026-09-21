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

type ContainerKind string

const (
	ContainerKindAccount  ContainerKind = ""
	ContainerKindCampaign ContainerKind = "campaign"
)

func (k ContainerKind) Valid() bool {
	return k == ContainerKindAccount || k == ContainerKindCampaign
}

type SearchEntriesInput struct {
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

	ResponsibleUserID     string
	ResponsibleUnassigned bool

	AssigneeOverrideUserID string

	Page     int
	PageSize int
}

type SearchByFilterInput struct {
	WorkspaceID string

	DepartmentIDs          []string
	RestrictDepartments    bool
	AssigneeOverrideUserID string

	AssignedUserID string

	Filter crmfilter.Filter

	SortField string
	SortOrder string

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
	EntryID               string
	EntryType             shared.EntryType
	CampaignID            string
	CampaignName          string
	LeadID                string
	LeadName              string
	LeadNumber            string
	BusinessPhoneID       string
	UnreadCount           int64
	LastMessageText       string
	LastMessageType       MessageType
	LastMessageAt         time.Time
	LastMessageFrom       string
	HasMedia              bool
	MediaType             MediaType
	MatchedMessages       []MatchedMessageResult
	TotalMatches          int
	AssignedUserID        string
	AgentID               string
	WorkflowID            string
	AgentResponsesEnabled bool
	WorkflowEnabled       bool
	AutomationEnabled     *bool
	ConversationStatus    string
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

	SearchEntriesByFilter(input SearchByFilterInput) ([]EntryWithLastMessage, int64, error)

	SearchMessagesByEntry(input SearchMessagesByEntryInput) ([]*Message, int64, error)

	GetEntryLastMessage(entryID string, entryType shared.EntryType) (*EntryWithLastMessage, error)

	CountByEntry(entryID string, entryType shared.EntryType) (int64, error)
	CountInboundByEntry(entryID string, entryType shared.EntryType) (int64, error)

	GetByWhatsAppMessageID(wamid string) (*Message, error)

	GetByExternalMessageID(entryType shared.EntryType, externalID string) (*Message, error)

	GetByEntryAndExternalMessageID(entryType shared.EntryType, entryID, externalID string) (*Message, error)
	UpdateDeliveryStatus(wamid string, status DeliveryStatus) error
	UpdateDeliveryStatusWithReason(wamid string, status DeliveryStatus, errorCode int, errorMessage string) error
	UpdateDeliveryReceipt(wamid string, receipt DeliveryReceipt) error

	ClearAll() error
}
