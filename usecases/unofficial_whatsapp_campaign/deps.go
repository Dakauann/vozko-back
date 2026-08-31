// Package unofficial_whatsapp_campaign runs bulk outbound over linked-device
// WhatsApp sessions.
//
// It mirrors usecases/whatsapp_campaign file for file, and everything the two
// genuinely share — the lifecycle state machine, the metrics algebra, the queue
// plumbing, the confirmation codes — has already been lifted into
// domain/campaign and usecases/campaignqueue. What is left here is what the
// transport actually makes different:
//
//   - no template and no balance, so nothing reserves, debits or refunds;
//   - a send has to look human, so there is pacing, a daily cap and a warmup ramp;
//   - a number can be BANNED, so there is a circuit breaker;
//   - the entry points at a conversation instead of being one.
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

// The narrow ports this package needs from elsewhere.
//
// Declared here as interfaces rather than taking the concrete services, so a
// test double is a few lines and so the campaign package does not acquire a
// dependency on the whole conversation stack to send one message.

// CampaignSender delivers one campaign message through the channel adapter.
//
// Satisfied by conversation_usecase.MessageSenderService. Named as a port here
// because the campaign must not be able to reach any OTHER send path: bypassing
// the adapter would skip pacing, restriction caching and the transcript.
type CampaignSender interface {
	SendCampaignMessage(in conversation_usecase.SendCampaignMessageInput) (*conversation.Message, error)
}

// Assigner puts a conversation into the round-robin.
//
// Called at SEND time, not on first reply. That is what makes a campaign a set
// of real conversations with owners rather than a fire-and-forget log, and it is
// what the official campaign already does.
type Assigner interface {
	EnsureAssignment(entryID, entryType, accountID string) string
}

// SpamGuard is the workspace's own cooldown on re-contacting a lead.
//
// The SAME guard the official campaign and both cold-outbound dialogs consult,
// so one workspace setting governs every way of reaching somebody. A campaign
// with its own copy would be a documented way to message a person the rest of
// the product had just refused to message.
type SpamGuard interface {
	// ShouldSkip reports whether this lead was reached from this sender too
	// recently.
	ShouldSkip(ctx context.Context, workspaceID, leadID, senderID string) bool
	// Record notes a successful send so later campaigns honour the cooldown.
	Record(leadID, senderID, campaignID string) error
	// SkipMany reports which of these leads are inside the cooldown, in one
	// query. Used at import so a campaign shows its spam skips before it starts.
	SkipMany(ctx context.Context, workspaceID string, leadIDs []string, senderID string) map[string]bool
}

// InstanceGateway is everything the campaign needs from the channel itself.
//
// One port rather than four repositories, because every call site needs the
// instance AND its server to build a provider ref, and threading both through
// each use case duplicated the same two lookups eleven times.
type InstanceGateway interface {
	// Instance loads one connected number.
	Instance(ctx context.Context, instanceID string) (*uw.Instance, error)
	// Ref builds the provider handle for it.
	Ref(ctx context.Context, instance *uw.Instance) (uw.InstanceRef, error)
	// CheckNumbers asks WhatsApp which of these numbers are registered.
	CheckNumbers(ctx context.Context, ref uw.InstanceRef, numbers []string) ([]uw.NumberCheck, error)
	// MessagingLimits asks WhatsApp itself whether this number may start new
	// conversations right now.
	MessagingLimits(ctx context.Context, ref uw.InstanceRef) (*uw.Restriction, error)
	// CacheRestriction stores what WhatsApp answered, so every later send is
	// refused before it reaches the provider.
	CacheRestriction(ctx context.Context, instanceID string, r uw.Restriction) error
	// Resolve turns a verified number into the contact and conversation the CRM
	// already knows.
	Resolve(ctx context.Context, instance *uw.Instance, in uwuc.ResolveInput) (*uwuc.Resolved, error)
}

// EntryBroadcaster pushes a live update to open inboxes.
type EntryBroadcaster interface {
	BroadcastEntryUpdate(entryID string, entryType string, payload interface{})
}

// MetricRecorder meters send volume.
//
// Volume only — no money. It is what makes pricing addable later without a data
// gap, and it is deliberately the ONLY commercial hook in the package.
type MetricRecorder interface {
	RecordCampaignSend(in RecordSendMetric)
}

// RecordSendMetric is one metered send.
type RecordSendMetric struct {
	WorkspaceID       string
	CampaignID        string
	EntryID           string
	InstanceID        string
	VariantIndex      int
	ProviderMessageID string
}

// WorkflowTrigger fires the campaign-sent trigger.
type WorkflowTrigger interface {
	CampaignSent(in CampaignSentTrigger)
}

// CampaignSentTrigger is the payload workflows branch on.
type CampaignSentTrigger struct {
	WorkspaceID       string
	CampaignID        string
	EntryID           string
	ConversationID    string
	PhoneNumber       string
	ProviderMessageID string
}

// LeadResolver is the ONE thing the campaign needs from the lead repository.
//
// A narrow port rather than lead.Repository, which has twenty methods this
// package will never call. Depending on the whole interface would mean every
// test double here reimplements the CRM's entire lead surface to exercise an
// import, and it would let a future change reach for a lead query from inside a
// campaign — which belongs in the CRM, not here.
type LeadResolver interface {
	// FindOrCreateMany resolves numbers to leads, keyed by the normalized
	// number. The SAME bridge the official campaign and the inbound path use, so
	// a contact reached by a campaign is the contact the CRM already knows.
	FindOrCreateMany(workspaceID string, inputs []lead.BulkLeadInput) (map[string]*lead.Lead, error)
}

// campaignRepos groups the two repositories every use case in this package
// needs, so each constructor takes one value instead of two.
type campaignRepos struct {
	campaigns uwc.Repository
	entries   uwc.EntryRepository
}
