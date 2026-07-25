package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WorkspaceDepartment struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID string         `gorm:"type:uuid;not null;index:idx_dept_workspace"`
	Name        string         `gorm:"not null;size:255"`
	Description string         `gorm:"size:500"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`

	Workspace Workspace                   `gorm:"foreignKey:WorkspaceID;references:ID"`
	Members   []WorkspaceDepartmentMember `gorm:"foreignKey:DepartmentID;references:ID"`
}

func (WorkspaceDepartment) TableName() string { return "workspace_departments" }

func (d *WorkspaceDepartment) BeforeCreate(tx *gorm.DB) error {
	if d.ID == "" {
		d.ID = uuid.New().String()
	}
	return nil
}

type WorkspaceDepartmentMember struct {
	ID           string    `gorm:"primaryKey;type:uuid"`
	DepartmentID string    `gorm:"type:uuid;not null;uniqueIndex:idx_dept_member_unique,priority:1"`
	MemberID     string    `gorm:"type:uuid;not null;uniqueIndex:idx_dept_member_unique,priority:2;index:idx_dept_member_member"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`

	Department WorkspaceDepartment `gorm:"foreignKey:DepartmentID;references:ID"`
	Member     WorkspaceMember     `gorm:"foreignKey:MemberID;references:ID"`
}

func (WorkspaceDepartmentMember) TableName() string { return "workspace_department_members" }

func (dm *WorkspaceDepartmentMember) BeforeCreate(tx *gorm.DB) error {
	if dm.ID == "" {
		dm.ID = uuid.New().String()
	}
	return nil
}
