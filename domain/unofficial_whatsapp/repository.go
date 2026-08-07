package unofficial_whatsapp

import (
	"context"
	"time"

	"vozko/domain/shared"
)

// ServerRepository persists the provider hosts we place instances on.
type ServerRepository interface {
	Create(ctx context.Context, s *Server) error
	Update(ctx context.Context, s *Server) error
	FindByID(ctx context.Context, id string) (*Server, error)
	// FindByBaseURL backs the idempotent seeding of the platform host from
	// configuration: booting twice must not create the host twice.
	FindByBaseURL(ctx context.Context, baseURL string) (*Server, error)

	// ListPlacementCandidates returns hosts a workspace may place on, least
	// loaded first, so placement spreads rather than filling one host.
	//
	// It returns platform-owned hosts plus that workspace's own, and never
	// another workspace's, which is the tenancy rule the admin token makes
	// non-negotiable.
	ListPlacementCandidates(ctx context.Context, workspaceID string) ([]*Server, error)
	ListAll(ctx context.Context) ([]*Server, error)

	// ClaimCapacity atomically increments InUse when the host still has room,
	// returning false when it does not.
	//
	// Compare-and-swap rather than read-then-write: two concurrent connects on
	// the last free slot would otherwise both succeed here and one would fail at
	// the host, after the tenant had already been told it worked.
	ClaimCapacity(ctx context.Context, serverID string) (claimed bool, err error)
	ReleaseCapacity(ctx context.Context, serverID string) error
	// SyncCapacity rewrites InUse from an authoritative count. A counter that
	// drifts silently is worse than no counter, so the health cron reconciles it
	// against the host's own instance list.
	SyncCapacity(ctx context.Context, serverID string, inUse int) error
	RecordHealth(ctx context.Context, serverID string, healthyAt *time.Time, lastError string) error
}

// InstanceRepository persists connected numbers.
type InstanceRepository interface {
	Create(ctx context.Context, i *Instance) error
	Update(ctx context.Context, i *Instance) error

	// UpdateStatus is a narrow write rather than a full-row save so a status
	// change from the health cron cannot clobber a concurrent config edit.
	UpdateStatus(ctx context.Context, id string, status Status, reason string) error
	// UpdateSession records what a status poll learned: identity, state and
	// disconnection detail, in one write.
	UpdateSession(ctx context.Context, id string, in SessionUpdate) error
	UpdateRestriction(ctx context.Context, id string, r Restriction) error
	SetWebhookRegistered(ctx context.Context, id string, at time.Time) error
	// RotateDeliveryToken replaces the webhook credential. Both the token and
	// its digest move together or the endpoint stops resolving.
	RotateDeliveryToken(ctx context.Context, id, token, tokenHash string) error

	FindByID(ctx context.Context, id string) (*Instance, error)
	// FindByDeliveryTokenHash resolves the tenant for an inbound webhook.
	//
	// The hot path, and it must include every non-deleted instance regardless of
	// status: an instance that just dropped is exactly the one whose next event
	// matters.
	FindByDeliveryTokenHash(ctx context.Context, tokenHash string) (*Instance, error)
	// FindByJID enforces "one WhatsApp number, one workspace" at connect time,
	// before the unique index has to.
	FindByJID(ctx context.Context, jid string) (*Instance, error)
	FindByProviderInstanceID(ctx context.Context, serverID, providerInstanceID string) (*Instance, error)

	ListByWorkspace(ctx context.Context, in ListInstancesInput) (*shared.PaginatedResult[*Instance], error)
	ListByServer(ctx context.Context, serverID string) ([]*Instance, error)
	// ListForHealthCheck returns instances whose session state has not been
	// confirmed since `before`, oldest first, so the cron makes progress even
	// under a per-run cap.
	//
	// "Confirmed" means by ANY authoritative signal, not only by a poll: the
	// provider pushes a `connection` event on every state change, and the
	// handler for it stamps the same clock. An instance that just reported over
	// the webhook is therefore skipped here automatically, which is what keeps
	// this job a backstop rather than a duplicate.
	ListForHealthCheck(ctx context.Context, before time.Time, limit int) ([]*Instance, error)
	// ListConnected returns every live instance, for the integrity sweep.
	//
	// Separate from ListForHealthCheck because it answers a different question:
	// not "has this session been confirmed recently" but "is our webhook still
	// registered, and is WhatsApp restricting this number" — neither of which
	// any event can tell us, so recency of contact is irrelevant.
	ListConnected(ctx context.Context, limit int) ([]*Instance, error)
	CountByServer(ctx context.Context, serverID string) (int, error)
	// CountByWorkspace is what the entitlement gate measures against.
	//
	// It counts EVERY non-deleted instance, not only connected ones, and that is
	// the point: a provisioned instance holds a slot on a host whether or not its
	// session is currently alive — the host's own capacity counter is claimed at
	// provisioning and released at deletion, never at disconnect. Counting only
	// live sessions would let a workspace provision without limit by letting its
	// numbers drop.
	CountByWorkspace(ctx context.Context, workspaceID string) (int, error)

	Delete(ctx context.Context, id string) error
}

// SessionUpdate is what one status poll learned.
//
// Status is a pointer because "the provider returned a state we do not
// recognise" must leave the stored status alone rather than guess at it.
type SessionUpdate struct {
	Status               *Status
	StatusReason         string
	JID                  string
	LID                  string
	PhoneNumber          string
	ProfileName          string
	ProfilePicURL        string
	IsBusinessAcct       bool
	Platform             string
	ConnectedAt          *time.Time
	LastDisconnectAt     *time.Time
	LastDisconnectReason string
	PolledAt             time.Time
}

type ListInstancesInput struct {
	WorkspaceID string
	Search      string
	Status      *Status
	// Scope limits the result to the caller's departments.
	//
	// The zero value is UNRESTRICTED, which is correct for the internal callers
	// that legitimately see every number — the health cron, capacity
	// reconciliation, the entitlement count. Only the HTTP path narrows it, and
	// it does so from the platform authorizer's answer rather than deriving
	// membership itself.
	Scope   DepartmentScope
	Options shared.QueryOptions
}

// ContactRepository persists the people on the other side of our numbers.
type ContactRepository interface {
	// FindOrCreate resolves a contact by (instance, jid), creating it on first
	// contact.
	//
	// It also reconciles the two identifier forms: a contact first seen under a
	// LID and later under a phone JID is ONE person, and creating a second row
	// would split a single real chat into two CRM conversations.
	FindOrCreate(ctx context.Context, in FindOrCreateContactInput) (*Contact, error)
	FindByID(ctx context.Context, id string) (*Contact, error)
	// FindByIDs batch-loads one page of inbox senders, so hydrating identity
	// costs one query rather than one per conversation.
	FindByIDs(ctx context.Context, ids []string) ([]*Contact, error)
	FindByJID(ctx context.Context, instanceID, jid string) (*Contact, error)
	// FindByHandles batch-loads the people who WROTE a page of group messages.
	//
	// A message row stores its author as a handle (Contact.Handle) rather than a
	// contact id, so reading a group thread back needs the reverse direction of
	// FindByIDs. Batched for the same reason: a lookup per bubble would make
	// opening a busy group an N+1.
	FindByHandles(ctx context.Context, instanceID string, handles []string) ([]*Contact, error)

	UpdateProfile(ctx context.Context, id string, p ContactProfile) error
	SetBlocked(ctx context.Context, id string, blocked bool, at time.Time) error
	// LinkLead attaches the CRM lead this contact resolved to.
	LinkLead(ctx context.Context, id, leadID string) error
}

// FindOrCreateContactInput identifies a conversation subject and seeds what the
// event already told us, so a first message yields a named subject with no extra
// call.
type FindOrCreateContactInput struct {
	WorkspaceID string
	InstanceID  string
	JID         string
	LID         string
	PhoneNumber string
	Name        string
	// IsGroup marks the subject as a group chat. Set at creation and never
	// changed: a JID does not stop being a group.
	IsGroup bool
}

// ContactProfile is the mutable, refreshable part of a conversation subject.
//
// FetchedAt is written on EVERY attempt, including failures. A subject whose
// profile read errors or returns nothing must not be retried on the next
// inbound message and the one after that: the TTL is the budget, and a fetch
// that only stamps on success turns every unphotographed contact into a
// permanent per-message provider call.
type ContactProfile struct {
	Name         string
	ContactName  string
	VerifiedName string
	PictureURL   string
	// PictureSourceURL is the provider url PictureURL was re-hosted from, and is
	// written together with it so the pair can never disagree about which photo
	// the stored copy is.
	PictureSourceURL string
	IsBusiness       bool
	FetchedAt        time.Time
}

// ConversationRepository persists conversation entries.
//
// The method set is deliberately identical in shape to the other channels': the
// conversation stack keys on (entry_id, entry_type) and wires these by
// reference, so a divergence here would mean re-implementing services that are
// already channel-agnostic.
type ConversationRepository interface {
	FindOrCreate(ctx context.Context, in FindOrCreateConversationInput) (*Conversation, error)
	FindByID(ctx context.Context, id string) (*Conversation, error)
	FindByChatID(ctx context.Context, instanceID, chatID string) (*Conversation, error)

	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
	ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error)

	RecordInbound(ctx context.Context, id string, at time.Time) error
	RecordOutbound(ctx context.Context, id string, at time.Time) error
	SetStatus(ctx context.Context, id, status, closeSource, closeReason string, closedAt *time.Time) error
	// SetAutomationEnabled is the per-conversation override an operator flips
	// when taking over. A nil value CLEARS the override so the conversation
	// inherits the instance switch again, which is a different state from an
	// explicit false.
	SetAutomationEnabled(ctx context.Context, id string, enabled *bool) error
	StatusForEntry(ctx context.Context, id string) (string, error)

	// CountByStatus powers the inbox status chips. Without it the header reads
	// "no work here" while the list below shows work.
	CountByStatus(ctx context.Context, workspaceID, instanceID string) (map[string]int64, error)
}

type FindOrCreateConversationInput struct {
	WorkspaceID string
	InstanceID  string
	ContactID   string
	ChatID      string
	IsGroup     bool
}

// GroupRepository persists the group read model.
//
// Deliberately separate from ContactRepository even though a group is also a
// conversation subject: the inbox reads subjects on every render and must never
// pay for a roster it does not display, while the group panel reads a roster and
// does not care about inbox projections. One repository serving both would put
// the expensive half behind the hot half.
type GroupRepository interface {
	// Upsert writes one synced group and replaces its roster atomically.
	//
	// Replace rather than merge: a member who left is absent from the new roster
	// and no diff we could compute locally would remove them. A roster that only
	// ever grows is worse than no roster, because it reads as authoritative.
	Upsert(ctx context.Context, g *Group) error

	FindByJID(ctx context.Context, instanceID, jid string) (*Group, error)
	FindByID(ctx context.Context, id string) (*Group, error)
	// ListByInstance returns the groups a number belongs to, most recently
	// active first. Rosters are NOT loaded: a list view shows names.
	ListByInstance(ctx context.Context, instanceID string) ([]*Group, error)
	// Participants loads one group's roster.
	Participants(ctx context.Context, groupID string) ([]GroupParticipant, error)

	// MarkStale records that something about this group changed, without saying
	// what. This is how the provider's `groups` webhook is consumed — as a
	// signal, never as a payload — and it is a no-op for a group we do not track.
	MarkStale(ctx context.Context, instanceID, jid string, at time.Time) error

	// LinkParticipantContacts attaches contact rows to roster entries whose
	// phone number we already know from a direct chat.
	//
	// Run after a roster sync rather than during it, and best-effort: the link
	// only powers "open the direct conversation" in the roster, and a member we
	// have never spoken to legitimately has none.
	LinkParticipantContacts(ctx context.Context, groupID, instanceID string) error

	Delete(ctx context.Context, id string) error
}

// ProcessedEventRepository is the durable webhook dedup store, identical in
// shape to the other channels' so this one inherits the same at-least-once
// guarantees rather than reimplementing them.
type ProcessedEventRepository interface {
	Claim(ctx context.Context, key, channel, instanceID string) (claimed bool, err error)
	PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}
