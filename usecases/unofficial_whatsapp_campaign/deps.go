package unofficial_whatsapp_campaign

import (
	"context"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	conversation_usecase "vozko/usecases/conversation"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

type CampaignSender interface {
	SendCampaignMessage(in conversation_usecase.SendCampaignMessageInput) (*conversation.Message, error)
}

type Assigner interface {
	EnsureAssignment(entryID, entryType, accountID string) string
}

type SpamGuard interface {
	ShouldSkip(ctx context.Context, workspaceID, leadID, senderID string) bool
	Record(leadID, senderID, campaignID string) error
	SkipMany(ctx context.Context, workspaceID string, leadIDs []string, senderID string) map[string]bool
}

type InstanceGateway interface {
	Instance(ctx context.Context, instanceID string) (*uw.Instance, error)
	Ref(ctx context.Context, instance *uw.Instance) (uw.InstanceRef, error)
	CheckNumbers(ctx context.Context, ref uw.InstanceRef, numbers []string) ([]uw.NumberCheck, error)
	MessagingLimits(ctx context.Context, ref uw.InstanceRef) (*uw.Restriction, error)
	CacheRestriction(ctx context.Context, instanceID string, r uw.Restriction) error
	Resolve(ctx context.Context, instance *uw.Instance, in uwuc.ResolveInput) (*uwuc.Resolved, error)
}

type EntryBroadcaster interface {
	BroadcastEntryUpdate(entryID string, entryType string, payload interface{})
}

type MetricRecorder interface {
	RecordCampaignSend(in RecordSendMetric)
}

type RecordSendMetric struct {
	WorkspaceID       string
	CampaignID        string
	EntryID           string
	InstanceID        string
	VariantIndex      int
	ProviderMessageID string
}

type WorkflowTrigger interface {
	CampaignSent(in CampaignSentTrigger)
}

type CampaignSentTrigger struct {
	WorkspaceID       string
	CampaignID        string
	EntryID           string
	ConversationID    string
	PhoneNumber       string
	ProviderMessageID string
}

type LeadResolver interface {
	FindOrCreateMany(workspaceID string, inputs []lead.BulkLeadInput) (map[string]*lead.Lead, error)
}

type campaignRepos struct {
	campaigns uwc.Repository
	entries   uwc.EntryRepository
}
