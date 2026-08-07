package unofficial_whatsapp

import (
	"errors"
	"strings"
	"time"
)

// The group read model.
//
// A group chat's IDENTITY (name, picture, blocked) lives on Contact, because
// that is what every conversation surface in the CRM asks for and a group
// answers it exactly like a person does. What lives here is everything that
// only a group has: who is in it, who runs it, whether we may post, and how
// stale that knowledge is.
//
// Splitting it this way is what keeps the inbox query untouched. The hottest
// read path in the product joins one subject table and projects four columns;
// making it aware of groups would have meant a polymorphic key or a UNION in
// exactly the place that can least afford one.
//
// The provider gives no push we can trust for any of this — the webhook's
// `groups` payload is documented only as "a map, the shape varies" — so every
// field below is a CACHE of an explicit read, with a staleness clock and an
// invalidation flag rather than a subscription.

var (
	ErrGroupNotFound      = errors.New("unofficial whatsapp group not found")
	ErrNotAGroupJID       = errors.New("that chat id does not address a group")
	ErrNotGroupAdmin      = errors.New("the connected number is not an admin of this group")
	ErrGroupNameRequired  = errors.New("a group name is required")
	ErrGroupNameTooLong   = errors.New("group name exceeds the 25 character limit")
	ErrGroupTopicTooLong  = errors.New("group description exceeds the 512 character limit")
	ErrNoParticipants     = errors.New("at least one participant is required")
	ErrInvalidGroupAction = errors.New("invalid group participant action")
)

// UnnamedGroupLabel is what a group with no synced subject renders as.
//
// Portuguese, matching the other operator-facing literals this channel emits
// (see placeholderForEmptyMessage). It is a transient state — the sync that
// follows the first inbound message replaces it — but a transient blank row is
// indistinguishable from a broken one.
const UnnamedGroupLabel = "Grupo"

// Provider-enforced limits, from the group endpoints' own schemas. Ours to
// check first so an operator gets a named error instead of a 400 the UI cannot
// explain.
const (
	MaxGroupNameRunes  = 25
	MaxGroupTopicRunes = 512
)

// GroupMetadataTTL is how long a synced roster stays authoritative.
//
// Deliberately long, and deliberately not a cron. Every refresh is a provider
// call on the same instance the send budget belongs to, and a group's name and
// membership change far less often than its messages arrive. What actually
// keeps it fresh is invalidation: a `groups` webhook or an operator pressing
// refresh marks the row stale and the next read re-syncs.
const GroupMetadataTTL = 24 * time.Hour

// GroupRole is a participant's standing in the group.
type GroupRole string

const (
	GroupRoleMember     GroupRole = "member"
	GroupRoleAdmin      GroupRole = "admin"
	GroupRoleSuperAdmin GroupRole = "superadmin"
)

// GroupAction is a membership mutation, matching the provider's own verbs.
type GroupAction string

const (
	GroupActionAdd     GroupAction = "add"
	GroupActionRemove  GroupAction = "remove"
	GroupActionPromote GroupAction = "promote"
	GroupActionDemote  GroupAction = "demote"
	GroupActionApprove GroupAction = "approve"
	GroupActionReject  GroupAction = "reject"
)

func (a GroupAction) Valid() bool {
	switch a {
	case GroupActionAdd, GroupActionRemove, GroupActionPromote,
		GroupActionDemote, GroupActionApprove, GroupActionReject:
		return true
	}
	return false
}

// Destructive reports whether this action removes access rather than granting
// it. The HTTP layer maps these onto ActionDelete instead of ActionUpdate:
// removing a customer from a group is not the same permission as renaming it.
func (a GroupAction) Destructive() bool {
	return a == GroupActionRemove || a == GroupActionReject
}

// Group is one WhatsApp group we participate in, as last read from the provider.
//
// It is keyed by (instance, jid) and NOT by conversation: we can be in a group
// that has not spoken yet, and a conversation is created by a message. Linking
// the two by JID keeps both directions valid.
type Group struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	InstanceID  string `json:"instanceId"`

	// JID is the group's address, "…@g.us". It is also the conversation's
	// ChatID, which is what joins this row to the inbox without a foreign key.
	JID string `json:"jid"`

	Subject     string `json:"subject"`
	Description string `json:"description,omitempty"`
	OwnerJID    string `json:"ownerJid,omitempty"`

	// Announce means only admins may post. Read by the composer: a member in an
	// announce-only group cannot reply, and finding that out from a failed send
	// is a worse experience than a disabled input with a reason.
	Announce bool `json:"announce"`
	// Locked means only admins may edit the group's name, picture and topic.
	Locked bool `json:"locked"`
	// JoinApproval means new members need admin approval.
	JoinApproval bool `json:"joinApproval"`
	// Ephemeral messages, and their timer in seconds.
	Ephemeral        bool `json:"ephemeral"`
	DisappearingSecs int  `json:"disappearingSeconds"`
	// Community marks a parent community rather than an ordinary group.
	Community bool `json:"community"`

	// WeAreAdmin and WeCanSend are the provider's answer about OUR connected
	// number, not about the group in the abstract. Every group action in the UI
	// is gated on them, so a control is never offered that is certain to fail.
	WeAreAdmin bool `json:"weAreAdmin"`
	WeCanSend  bool `json:"weCanSend"`

	// InviteLink is fetched only on request: it is a credential that lets
	// anyone holding it join, so it is never populated by a background sync.
	InviteLink string `json:"inviteLink,omitempty"`

	ParticipantCount int        `json:"participantCount"`
	GroupCreatedAt   *time.Time `json:"groupCreatedAt,omitempty"`

	// SyncedAt is when the provider last confirmed all of the above. Nil means
	// never, which reads as stale rather than as fresh.
	SyncedAt *time.Time `json:"syncedAt,omitempty"`
	// StaleAt marks the row as needing a re-read before the TTL would have
	// expired. This is how the `groups` webhook is consumed: as a signal that
	// something changed, never as the payload describing WHAT changed.
	StaleAt *time.Time `json:"-"`

	Participants []GroupParticipant `json:"participants,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// GroupParticipant is one member, as of the last roster sync.
//
// It carries the identity the provider gave and, when we already know the
// person from a direct chat, a link to their contact row. The link is resolved
// lazily and is allowed to be absent: most members of most groups have never
// messaged us, and creating a contact for each would put hundreds of rows that
// no CRM surface can act on into the table the dialer reads.
type GroupParticipant struct {
	ID      string `json:"id"`
	GroupID string `json:"groupId"`

	JID string `json:"jid"`
	LID string `json:"lid,omitempty"`
	// PhoneNumber is digits only, empty when the group hides it (LID-only
	// members are normal under WhatsApp's newer privacy model).
	PhoneNumber string `json:"phoneNumber,omitempty"`
	// DisplayName is what the group shows for this member.
	DisplayName string    `json:"displayName,omitempty"`
	Role        GroupRole `json:"role"`
	// ContactID links to the contact row for this person when one exists, so
	// the roster can offer "open the direct conversation".
	ContactID *string `json:"contactId,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// IsAdmin reports whether this member may administer the group.
func (p GroupParticipant) IsAdmin() bool {
	return p.Role == GroupRoleAdmin || p.Role == GroupRoleSuperAdmin
}

// Label is what the roster shows for a member. Never blank: a member with no
// name at all is still identifiable by their number, and a member with neither
// is at least identifiably anonymous rather than an empty row.
func (p GroupParticipant) Label() string {
	if name := strings.TrimSpace(p.DisplayName); name != "" {
		return name
	}
	if p.PhoneNumber != "" {
		return "+" + p.PhoneNumber
	}
	return p.JID
}

// Normalize canonicalises what the provider returned.
func (g *Group) Normalize() {
	g.JID = strings.TrimSpace(g.JID)
	g.Subject = strings.TrimSpace(g.Subject)
	g.Description = strings.TrimSpace(g.Description)
	g.OwnerJID = strings.TrimSpace(g.OwnerJID)
	for i := range g.Participants {
		p := &g.Participants[i]
		p.JID = strings.TrimSpace(p.JID)
		p.LID = strings.TrimSpace(p.LID)
		p.DisplayName = strings.TrimSpace(p.DisplayName)
		p.PhoneNumber = NormalizePhone(p.PhoneNumber)
		if p.PhoneNumber == "" {
			p.PhoneNumber = PhoneFromJID(p.JID)
		}
		if p.Role == "" {
			p.Role = GroupRoleMember
		}
	}
	if g.ParticipantCount == 0 {
		g.ParticipantCount = len(g.Participants)
	}
}

func (g *Group) Validate() error {
	if strings.TrimSpace(g.WorkspaceID) == "" {
		return ErrWorkspaceIDRequired
	}
	if !IsGroupJID(g.JID) {
		return ErrNotAGroupJID
	}
	return nil
}

// NeedsSync reports whether the cached metadata should be re-read.
//
// Three ways to be stale, and they are deliberately not collapsed: never
// synced, explicitly invalidated, or simply old. The middle one is what makes a
// rename visible in seconds instead of in a day.
func (g *Group) NeedsSync(now time.Time, ttl time.Duration) bool {
	if g == nil || g.SyncedAt == nil {
		return true
	}
	if g.StaleAt != nil && g.StaleAt.After(*g.SyncedAt) {
		return true
	}
	return now.Sub(*g.SyncedAt) > ttl
}

// CanPost reports whether our number may send into this group right now.
//
// Announce-only groups are the case this exists for: the send would be accepted
// by our own composer, refused by WhatsApp, and land in the transcript as a
// failure the operator cannot explain.
func (g *Group) CanPost() bool {
	if g == nil {
		// An unsynced group is not a reason to block a reply. The provider is
		// the authority on the refusal, and guessing "no" here would silence
		// every group in the window before its first sync completes.
		return true
	}
	if !g.Announce {
		return true
	}
	return g.WeAreAdmin || g.WeCanSend
}

// ValidateGroupName checks a rename before it costs a round trip.
func ValidateGroupName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ErrGroupNameRequired
	}
	if len([]rune(trimmed)) > MaxGroupNameRunes {
		return ErrGroupNameTooLong
	}
	return nil
}

// ValidateGroupTopic checks a description before it costs a round trip.
func ValidateGroupTopic(topic string) error {
	if len([]rune(strings.TrimSpace(topic))) > MaxGroupTopicRunes {
		return ErrGroupTopicTooLong
	}
	return nil
}
