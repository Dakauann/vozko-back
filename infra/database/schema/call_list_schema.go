package schema

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/datatypes"
)

type CallList struct {
	ID            string         `gorm:"primaryKey;type:uuid;uniqueIndex:ux_call_lists_workspace_id,priority:2"`
	WorkspaceID   string         `gorm:"type:uuid;not null;uniqueIndex:ux_call_lists_workspace_id,priority:1;index:idx_call_lists_workspace_created,priority:1"`
	Name          string         `gorm:"type:varchar(120);not null"`
	CreatedBy     string         `gorm:"type:uuid;not null"`
	AssigneeIDs   pq.StringArray `gorm:"column:assignee_ids;type:uuid[];not null;default:'{}'"`
	Status        string         `gorm:"type:varchar(16);not null;index:idx_call_lists_building,where:status = 'building'"`
	PhoneSource   string         `gorm:"type:varchar(16);not null"`
	PhoneLabel    OptionalText   `gorm:"type:varchar(16)"`
	Selected      int            `gorm:"not null;default:0"`
	ItemCount     int            `gorm:"not null;default:0"`
	ClosedCount   int            `gorm:"not null;default:0"`
	CalledCount   int            `gorm:"not null;default:0"`
	CallbackCount int            `gorm:"not null;default:0"`
	Skipped       datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	FailureCode   OptionalText   `gorm:"type:varchar(32)"`
	ClaimToken    OptionalText   `gorm:"type:varchar(64)"`
	HeartbeatAt   *time.Time     `gorm:"type:timestamptz"`
	Attempts      int            `gorm:"not null;default:0"`
	BuildCursor   OptionalText   `gorm:"type:uuid"`
	BuiltAt       *time.Time     `gorm:"type:timestamptz"`
	CreatedAt     time.Time      `gorm:"type:timestamptz;not null;index:idx_call_lists_workspace_created,priority:2"`
	UpdatedAt     time.Time      `gorm:"type:timestamptz;not null"`
}

func (CallList) TableName() string {
	return "call_lists"
}

type CallListItem struct {
	ID            string       `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string       `gorm:"type:uuid;not null;uniqueIndex:ux_call_list_items_reserver,priority:1,where:state = 'reserved'"`
	ListID        string       `gorm:"type:uuid;not null;index:idx_call_list_items_queue,priority:1;uniqueIndex:ux_call_list_items_list_lead,priority:1;uniqueIndex:ux_call_list_items_list_position,priority:1;index:idx_call_list_items_callbacks,priority:1,where:state = 'pending' AND callback_at IS NOT NULL"`
	LeadID        string       `gorm:"type:uuid;not null;uniqueIndex:ux_call_list_items_list_lead,priority:2;index:idx_call_list_items_lead"`
	Phone         string       `gorm:"type:varchar(32);not null"`
	Position      int          `gorm:"not null;index:idx_call_list_items_queue,priority:3;uniqueIndex:ux_call_list_items_list_position,priority:2"`
	State         string       `gorm:"type:varchar(16);not null;default:'pending';index:idx_call_list_items_queue,priority:2"`
	ReservedBy    OptionalText `gorm:"type:uuid;uniqueIndex:ux_call_list_items_reserver,priority:2,where:state = 'reserved'"`
	ReservedUntil *time.Time   `gorm:"type:timestamptz"`
	Disposition   OptionalText `gorm:"type:varchar(64)"`
	Note          OptionalText `gorm:"type:text"`
	Refusal       OptionalText `gorm:"type:varchar(32)"`
	CallbackAt    *time.Time   `gorm:"type:timestamptz;index:idx_call_list_items_callbacks,priority:2,where:state = 'pending' AND callback_at IS NOT NULL"`
	LastCallID    OptionalText `gorm:"type:uuid"`
	ClosedBy      OptionalText `gorm:"type:uuid"`
	ClosedAt      *time.Time   `gorm:"type:timestamptz"`
	CreatedAt     time.Time    `gorm:"type:timestamptz;not null"`
	UpdatedAt     time.Time    `gorm:"type:timestamptz;not null"`
}

func (CallListItem) TableName() string {
	return "call_list_items"
}
