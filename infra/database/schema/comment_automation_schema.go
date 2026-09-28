package schema

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

type CommentRule struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	Source      string `gorm:"size:32;not null;index:idx_comment_rule_scope,priority:1"`
	AccountID   string `gorm:"type:uuid;not null;index:idx_comment_rule_scope,priority:2"`
	ContainerID string `gorm:"size:96;not null;default:'';index"`

	Name    string `gorm:"size:120;not null"`
	Enabled bool   `gorm:"not null;default:true;index"`

	Match    string         `gorm:"size:16;not null;default:'contains'"`
	Keywords pq.StringArray `gorm:"type:text[]"`
	Actions  pq.StringArray `gorm:"type:text[]"`

	PublicReplyText  string `gorm:"type:text"`
	PrivateReplyText string `gorm:"type:text"`

	Priority int `gorm:"not null;default:0"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (CommentRule) TableName() string { return "comment_rules" }

func (r *CommentRule) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	return nil
}

type CommentPrivateReply struct {
	Source    string `gorm:"primaryKey;size:32"`
	CommentID string `gorm:"primaryKey;size:96"`
	AccountID string `gorm:"type:uuid;not null;index"`
	Status    string `gorm:"size:16;not null;default:'ATTEMPTED'"`

	RecipientRef *string `gorm:"size:64"`
	MessageID    *string `gorm:"type:text"`
	ErrorCode    int     `gorm:"default:0"`
	ErrorMessage string  `gorm:"size:500"`

	AttemptedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`
}

func (CommentPrivateReply) TableName() string { return "comment_private_replies" }
