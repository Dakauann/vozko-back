package unofficial_whatsapp_campaign

import (
	"time"

	"vozko/domain/campaign"
	"vozko/domain/shared"
)

// QueueNamespace keys this channel's queue topic and coordination keys.
//
// A distinct prefix from the Cloud API campaign's is what stops the two
// channels' campaigns sharing a pause flag or a completion counter.
var QueueNamespace = campaign.Namespace{
	Topic: "unofficial_whatsapp_campaign_dispatch",
	Key:   "campaign:unofficial_whatsapp",
}

type ListCampaignsInput struct {
	WorkspaceID   string
	DepartmentIDs []string
	InstanceIDs   []string
	Search        string
	Status        campaign.Status
	Archived      *bool
	Options       shared.QueryOptions
}

type ListEntriesInput struct {
	CampaignID string
	Status     campaign.SendStatus
	Number     string
	Search     string
	StageID    string
	ErrorCode  int
	Options    shared.QueryOptions
}

// WorkspaceSummaryFilter selects which campaigns feed the workspace rollup.
// Mirrors the official channel's filter so the summary bar asks both the same
// question; there is no Type here because this channel has no campaign types.
type WorkspaceSummaryFilter struct {
	WorkspaceID   string
	DepartmentIDs []string
	InstanceIDs   []string
	CreatedFrom   *time.Time
	CreatedTo     *time.Time
}

type Repository interface {
	Create(c *Campaign) error
	Update(campaignID string, c *Campaign) error
	Delete(campaignID string) error
	FindByID(campaignID string) (*Campaign, error)
	List(input ListCampaignsInput) (*shared.PaginatedResult[*Campaign], error)
	ListByStatus(status campaign.Status) ([]*Campaign, error)
	// ListRunningByInstance backs the circuit breaker: when WhatsApp restricts a
	// number, every campaign on it has to stop, not just the one that noticed.
	ListRunningByInstance(instanceID string) ([]*Campaign, error)
	ListScheduledToStart(at time.Time, limit int) ([]*Campaign, error)
	// UpdateStatus is a compare-and-swap when allowed is non-empty, reporting
	// whether it changed a row.
	UpdateStatus(campaignID string, status campaign.Status, allowed ...campaign.Status) (bool, error)
	UpdateStatusReason(campaignID, reason string) error
	UpdateResetCode(campaignID, code string) error
	UpdateClearCode(campaignID, code string) error
}

// RecordSendInput is everything one successful send writes back onto its entry.
//
// Grouped into a struct rather than eight positional arguments because they are
// written together in one UPDATE, and because a positional list of six strings
// is a swap waiting to happen.
type RecordSendInput struct {
	ContactID         string
	ConversationID    string
	ProviderMessageID string
	MessageID         string
	VariantIndex      int
	SentAt            time.Time
}

type EntryRepository interface {
	CreateMany(entries []Entry) ([]Entry, error)
	FindByID(entryID string) (*Entry, error)
	// FindByProviderMessageID is the delivery-status hook: a webhook carries the
	// provider's message id and nothing else that identifies a campaign.
	FindByProviderMessageID(providerMessageID string) (*Entry, error)
	FindByCampaignAndLead(campaignID, leadID string) (*Entry, error)
	// FindLatestByConversationID answers "which campaign owns replies here".
	//
	// Inbound on this channel carries a conversation, never a campaign: the
	// entry POINTS AT a conversation instead of being one (see Entry's doc), so
	// the link has to be walked backwards. When two campaigns have targeted the
	// same contact on the same instance the most recently SENT one wins — it is
	// the message the person is answering.
	FindLatestByConversationID(conversationID string) (*Entry, error)
	Delete(entryID string) error
	DeleteByCampaignID(campaignID string) error

	List(input ListEntriesInput) (*shared.PaginatedResult[*EntryWithLead], error)
	ListByStatus(campaignID string, status campaign.SendStatus, limit int) ([]Entry, error)
	ListRecentlyUpdated(campaignID string, limit int) ([]Entry, error)

	CountByStatus(campaignID string) (*campaign.Counts, error)
	// CountByStatusForCampaigns aggregates many campaigns in ONE query, so a
	// list page does not become an N+1 as a workspace accumulates campaigns.
	CountByStatusForCampaigns(campaignIDs []string) (map[string]*campaign.Counts, error)

	UpdateStatus(entryID string, status campaign.SendStatus, providerMessageID string, errorCode int, errorMessage string) error
	// UpdateStatusByProviderMessageID advances an entry from a delivery webhook.
	UpdateStatusByProviderMessageID(providerMessageID string, status campaign.SendStatus) error
	RecordCheck(entryID, jid string, at time.Time, onWhatsApp bool) error
	RecordSend(entryID string, in RecordSendInput) error

	// UpdateEntryDetails edits one row BY ID.
	//
	// Deliberately not UpsertEntries: that conflicts on (campaign_id, lead_id),
	// so changing an entry's number — which changes its lead — would insert a
	// second row rather than move the existing one, and the old row would stay
	// in the campaign.
	UpdateEntryDetails(entryID string, in UpdateEntryDetails) error

	ResetAllStatuses(campaignID string) (int64, error)
	UpsertEntries(campaignID string, entries []Entry) error

	// ConversationIDsForCampaign backs the campaign-scoped CRM view and the
	// clear-history wipe.
	ConversationIDsForCampaign(campaignID string) ([]string, error)
}

// SummaryAggregator rolls entry statuses up to the workspace level in one query.
//
// Separate from EntryRepository, mirroring the official channel, so adding the
// summary does not force every repository double to grow a method. There is no
// by-category half: that is a fact about templates, which this channel has none of.
// UpdateEntryDetails is the editable half of an entry.
//
// Status, provider ids and timestamps are absent on purpose: those belong to the
// send pipeline, and letting an edit touch them would let an operator mark a
// message delivered by typing.
type UpdateEntryDetails struct {
	LeadID    string
	Number    string
	Name      string
	Variables []string
	Metadata  map[string]interface{}
}

type SummaryAggregator interface {
	CountByStatusForWorkspace(filter WorkspaceSummaryFilter) (*campaign.Counts, error)
}
