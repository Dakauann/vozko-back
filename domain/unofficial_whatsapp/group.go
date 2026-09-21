package unofficial_whatsapp

import (
	"errors"
	"strings"
	"time"
)

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

const UnnamedGroupLabel = "Grupo"

const (
	MaxGroupNameRunes  = 25
	MaxGroupTopicRunes = 512
)

const GroupMetadataTTL = 24 * time.Hour

type GroupRole string

const (
	GroupRoleMember     GroupRole = "member"
	GroupRoleAdmin      GroupRole = "admin"
	GroupRoleSuperAdmin GroupRole = "superadmin"
)

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

func (a GroupAction) Destructive() bool {
	return a == GroupActionRemove || a == GroupActionReject
}

type Group struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	InstanceID  string `json:"instanceId"`

	JID string `json:"jid"`

	Subject     string `json:"subject"`
	Description string `json:"description,omitempty"`
	OwnerJID    string `json:"ownerJid,omitempty"`

	Announce         bool `json:"announce"`
	Locked           bool `json:"locked"`
	JoinApproval     bool `json:"joinApproval"`
	Ephemeral        bool `json:"ephemeral"`
	DisappearingSecs int  `json:"disappearingSeconds"`
	Community        bool `json:"community"`

	WeAreAdmin bool `json:"weAreAdmin"`
	WeCanSend  bool `json:"weCanSend"`

	InviteLink string `json:"inviteLink,omitempty"`

	ParticipantCount int        `json:"participantCount"`
	GroupCreatedAt   *time.Time `json:"groupCreatedAt,omitempty"`

	SyncedAt *time.Time `json:"syncedAt,omitempty"`
	StaleAt  *time.Time `json:"-"`

	Participants []GroupParticipant `json:"participants,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type GroupParticipant struct {
	ID      string `json:"id"`
	GroupID string `json:"groupId"`

	JID         string    `json:"jid"`
	LID         string    `json:"lid,omitempty"`
	PhoneNumber string    `json:"phoneNumber,omitempty"`
	DisplayName string    `json:"displayName,omitempty"`
	Role        GroupRole `json:"role"`
	ContactID   *string   `json:"contactId,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (p GroupParticipant) IsAdmin() bool {
	return p.Role == GroupRoleAdmin || p.Role == GroupRoleSuperAdmin
}

func (p GroupParticipant) Label() string {
	if name := strings.TrimSpace(p.DisplayName); name != "" {
		return name
	}
	if p.PhoneNumber != "" {
		return "+" + p.PhoneNumber
	}
	return p.JID
}

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

func (g *Group) NeedsSync(now time.Time, ttl time.Duration) bool {
	if g == nil || g.SyncedAt == nil {
		return true
	}
	if g.StaleAt != nil && g.StaleAt.After(*g.SyncedAt) {
		return true
	}
	return now.Sub(*g.SyncedAt) > ttl
}

func (g *Group) CanPost() bool {
	if g == nil {
		return true
	}
	if !g.Announce {
		return true
	}
	return g.WeAreAdmin || g.WeCanSend
}

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

func ValidateGroupTopic(topic string) error {
	if len([]rune(strings.TrimSpace(topic))) > MaxGroupTopicRunes {
		return ErrGroupTopicTooLong
	}
	return nil
}
