package schema

import (
	"time"

	"github.com/lib/pq"
)

type TeamChatThread struct {
	ID            string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string    `gorm:"type:uuid;not null;uniqueIndex:ux_team_chat_threads_direct,priority:1,where:direct_key <> ''"`
	Kind          string    `gorm:"type:varchar(16);not null"`
	Title         string    `gorm:"type:varchar(80);not null;default:''"`
	DirectKey     string    `gorm:"type:varchar(80);not null;default:'';uniqueIndex:ux_team_chat_threads_direct,priority:2,where:direct_key <> ''"`
	CreatedBy     string    `gorm:"type:uuid;not null"`
	LastSeq       int64     `gorm:"not null;default:0"`
	LastMessageAt time.Time `gorm:"type:timestamptz;not null"`
	CreatedAt     time.Time `gorm:"type:timestamptz;not null"`
}

func (TeamChatThread) TableName() string { return "team_chat_threads" }

type TeamChatMember struct {
	ThreadID    string     `gorm:"primaryKey;type:uuid"`
	UserID      string     `gorm:"primaryKey;type:uuid;index:idx_team_chat_members_person,priority:2,where:left_at IS NULL"`
	WorkspaceID string     `gorm:"type:uuid;not null;index:idx_team_chat_members_person,priority:1,where:left_at IS NULL"`
	Role        string     `gorm:"type:varchar(16);not null"`
	LastReadSeq int64      `gorm:"not null;default:0"`
	Muted       bool       `gorm:"not null;default:false"`
	JoinedAt    time.Time  `gorm:"type:timestamptz;not null"`
	LeftAt      *time.Time `gorm:"type:timestamptz"`
}

func (TeamChatMember) TableName() string { return "team_chat_members" }

type TeamChatMessage struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	ThreadID    string         `gorm:"type:uuid;not null;uniqueIndex:ux_team_chat_messages_seq,priority:1;uniqueIndex:ux_team_chat_messages_client,priority:1,where:client_id <> ''"`
	WorkspaceID string         `gorm:"type:uuid;not null"`
	Seq         int64          `gorm:"not null;uniqueIndex:ux_team_chat_messages_seq,priority:2"`
	AuthorID    string         `gorm:"type:uuid;not null;uniqueIndex:ux_team_chat_messages_client,priority:2,where:client_id <> ''"`
	Kind        string         `gorm:"type:varchar(16);not null"`
	Body        string         `gorm:"type:text;not null;default:''"`
	Event       string         `gorm:"type:varchar(32);not null;default:''"`
	SubjectIDs  pq.StringArray `gorm:"type:uuid[];not null;default:'{}'"`
	ReplyToID   *string        `gorm:"type:uuid"`
	Mentions    pq.StringArray `gorm:"type:uuid[];not null;default:'{}'"`
	ClientID    string         `gorm:"type:varchar(128);not null;default:'';uniqueIndex:ux_team_chat_messages_client,priority:3,where:client_id <> ''"`
	CreatedAt   time.Time      `gorm:"type:timestamptz;not null"`
	EditedAt    *time.Time     `gorm:"type:timestamptz"`
	DeletedAt   *time.Time     `gorm:"type:timestamptz"`
}

func (TeamChatMessage) TableName() string { return "team_chat_messages" }

type TeamChatScheduledMessage struct {
	ID            string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string         `gorm:"type:uuid;not null"`
	ThreadID      string         `gorm:"type:uuid;not null;index:idx_team_chat_scheduled_author,priority:1,where:status = 'pending'"`
	AuthorID      string         `gorm:"type:uuid;not null;index:idx_team_chat_scheduled_author,priority:2,where:status = 'pending'"`
	Body          string         `gorm:"type:text;not null"`
	Mentions      pq.StringArray `gorm:"type:uuid[];not null;default:'{}'"`
	ReplyToID     *string        `gorm:"type:uuid"`
	SendAt        time.Time      `gorm:"type:timestamptz;not null;index:idx_team_chat_scheduled_due,where:status = 'pending'"`
	Status        string         `gorm:"type:varchar(16);not null"`
	FailureReason string         `gorm:"type:varchar(32);not null;default:''"`
	SentMessageID *string        `gorm:"type:uuid"`
	CreatedAt     time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt     time.Time      `gorm:"type:timestamptz;not null"`
}

func (TeamChatScheduledMessage) TableName() string { return "team_chat_scheduled_messages" }
