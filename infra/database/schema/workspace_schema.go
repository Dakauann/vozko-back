package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Workspace struct {
	ID        string         `gorm:"primaryKey;type:uuid"`
	OwnerID   string         `gorm:"type:uuid;index;uniqueIndex:idx_ws_owner_default,priority:1"`
	Name      string         `gorm:"not null;size:255"`
	IsDefault bool           `gorm:"not null;default:false;uniqueIndex:idx_ws_owner_default,priority:2"`
	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	Owner   User              `gorm:"foreignKey:OwnerID;references:ID"`
	Members []WorkspaceMember `gorm:"foreignKey:WorkspaceID;references:ID"`
}

func (Workspace) TableName() string { return "workspaces" }

func (w *Workspace) BeforeCreate(tx *gorm.DB) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	return nil
}

type WorkspaceMember struct {
	ID          string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID string  `gorm:"type:uuid;not null;index;uniqueIndex:idx_ws_member_ws_user,priority:1"`
	UserID      string  `gorm:"type:uuid;not null;index;uniqueIndex:idx_ws_member_ws_user,priority:2"`
	Role        string  `gorm:"not null;size:50;default:''"`
	RoleID      *string `gorm:"type:uuid;index"`
	// RingChannels: comma-separated set of endpoint channels that ring for this
	// member (e.g. "browser,branch"). New and existing rows default to all channels
	// (the field is omitted on insert so the DB default applies); updated via a
	// targeted column write.
	RingChannels string    `gorm:"not null;default:'browser,branch'"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"`

	Workspace  Workspace            `gorm:"foreignKey:WorkspaceID;references:ID"`
	User       User                 `gorm:"foreignKey:UserID;references:ID"`
	CustomRole *WorkspaceCustomRole `gorm:"foreignKey:RoleID;references:ID"`
}

func (WorkspaceMember) TableName() string { return "workspace_members" }

func (m *WorkspaceMember) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

type WorkspaceMemberPermission struct {
	ID        string    `gorm:"primaryKey;type:uuid"`
	MemberID  string    `gorm:"type:uuid;not null;index;index:idx_wmp_member_resource_action,priority:1"`
	Resource  string    `gorm:"not null;size:100;index:idx_wmp_member_resource_action,priority:2"`
	Action    string    `gorm:"not null;size:50;index:idx_wmp_member_resource_action,priority:3"`
	CreatedAt time.Time `gorm:"autoCreateTime"`

	Member WorkspaceMember `gorm:"foreignKey:MemberID;references:ID"`
}

func (WorkspaceMemberPermission) TableName() string { return "workspace_member_permissions" }

func (p *WorkspaceMemberPermission) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}

type WorkspaceInvite struct {
	ID            string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string    `gorm:"type:uuid;not null;index"`
	InviterID     string    `gorm:"type:uuid;not null"`
	Email         string    `gorm:"not null;size:255;index"`
	Role          string    `gorm:"not null;size:50;default:''"`
	RoleID        *string   `gorm:"type:uuid"`
	Status        string    `gorm:"not null;size:20;default:'pending';index"`
	Token         string    `gorm:"uniqueIndex;not null;size:255"`
	Permissions   string    `gorm:"type:text"`
	DepartmentIDs string    `gorm:"type:text"`
	ExpiresAt     time.Time `gorm:"not null"`
	CreatedAt     time.Time `gorm:"autoCreateTime"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime"`

	Workspace Workspace `gorm:"foreignKey:WorkspaceID;references:ID"`
	Inviter   User      `gorm:"foreignKey:InviterID;references:ID"`
}

func (WorkspaceInvite) TableName() string { return "workspace_invites" }

func (i *WorkspaceInvite) BeforeCreate(tx *gorm.DB) error {
	if i.ID == "" {
		i.ID = uuid.New().String()
	}
	return nil
}

type WorkspaceCustomRole struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID string    `gorm:"type:uuid;not null;index:idx_wcr_workspace"`
	Name        string    `gorm:"not null;size:100"`
	Description string    `gorm:"size:500"`
	Permissions string    `gorm:"type:text"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`

	Workspace Workspace `gorm:"foreignKey:WorkspaceID;references:ID"`
}

func (WorkspaceCustomRole) TableName() string { return "workspace_custom_roles" }

func (r *WorkspaceCustomRole) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}

type WorkspaceResourceAssignment struct {
	ID           string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string    `gorm:"type:uuid;not null;index:idx_wra_workspace_resource"`
	ResourceType string    `gorm:"not null;size:100;index:idx_wra_workspace_resource"`
	ResourceID   string    `gorm:"not null;type:uuid;index:idx_wra_workspace_resource"`
	MemberID     string    `gorm:"type:uuid;not null;index:idx_wra_member"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`

	Workspace Workspace       `gorm:"foreignKey:WorkspaceID;references:ID"`
	Member    WorkspaceMember `gorm:"foreignKey:MemberID;references:ID"`
}

func (WorkspaceResourceAssignment) TableName() string { return "workspace_resource_assignments" }

func (a *WorkspaceResourceAssignment) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}
