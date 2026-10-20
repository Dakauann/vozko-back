package schema

import "time"

type WorkflowSecret struct {
	ID             string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID    string    `gorm:"type:uuid;not null;uniqueIndex:ux_workflow_secrets_workspace_name,priority:1"`
	Name           string    `gorm:"size:64;not null;uniqueIndex:ux_workflow_secrets_workspace_name,priority:2"`
	EncryptedValue []byte    `gorm:"type:bytea;not null"`
	CreatedBy      string    `gorm:"size:64;not null;default:''"`
	UpdatedBy      string    `gorm:"size:64;not null;default:''"`
	CreatedAt      time.Time `gorm:"not null"`
	UpdatedAt      time.Time `gorm:"not null"`
}

func (WorkflowSecret) TableName() string { return "workflow_secrets" }
