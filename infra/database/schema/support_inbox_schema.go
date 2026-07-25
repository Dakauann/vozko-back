package schema

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SupportInbox struct {
	ID                   string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID          string         `gorm:"type:uuid;not null;index;index:idx_support_inbox_ws_del,priority:1"`
	Name                 string         `gorm:"size:255;not null"`
	AgentID              *string        `gorm:"type:uuid;index"`
	EnableAgentResponses bool           `gorm:"not null;default:false"`
	GreetingMessage      string         `gorm:"size:1024"`
	WidgetColor          string         `gorm:"size:20"`
	AllowedOrigins       StringArray    `gorm:"type:jsonb;default:'[]'"`
	PreChatFields        JSONRaw        `gorm:"type:jsonb;default:'[]'"`
	Archived             bool           `gorm:"default:false"`
	CreatedAt            time.Time      `gorm:"autoCreateTime"`
	UpdatedAt            time.Time      `gorm:"autoUpdateTime"`
	DeletedAt            gorm.DeletedAt `gorm:"index;index:idx_support_inbox_ws_del,priority:2"`
}

func (SupportInbox) TableName() string {
	return "support_inboxes"
}

func (s *SupportInbox) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

type SupportEntry struct {
	ID           string         `gorm:"primaryKey;type:uuid"`
	InboxID      string         `gorm:"type:uuid;not null;index;index:idx_se_inbox_del,priority:1"`
	ContactName  string         `gorm:"size:255"`
	ContactEmail string         `gorm:"size:320"`
	SourceURL    string         `gorm:"size:2048"`
	CreatedAt    time.Time      `gorm:"autoCreateTime"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime"`
	DeletedAt    gorm.DeletedAt `gorm:"index;index:idx_se_inbox_del,priority:2"`
	// LastMessageAt denormalizes the newest conversation_messages.created_at for this
	// entry. The inbox lists order and filter by it, which replaces a per-entry
	// JOIN LATERAL over conversation_messages that forced a full scan of every entry
	// in the workspace on each load. NULL means "no messages" — such entries are not
	// listed, matching the inner-join semantics the LATERAL had.
	LastMessageAt *time.Time `gorm:"column:last_message_at"`
}

func (SupportEntry) TableName() string {
	return "support_entries"
}

func (s *SupportEntry) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

type SupportSession struct {
	ID        string    `gorm:"primaryKey;type:uuid"`
	InboxID   string    `gorm:"type:uuid;not null;index"`
	EntryID   string    `gorm:"type:uuid;not null;index"`
	Token     string    `gorm:"type:text;not null;uniqueIndex:idx_support_session_token"`
	ExpiresAt time.Time `gorm:"type:timestamptz;not null;index"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

func (SupportSession) TableName() string {
	return "support_sessions"
}

func (s *SupportSession) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

type StringArray []string

func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return "[]", nil
	}
	b, err := json.Marshal(a)
	if err != nil {
		return nil, fmt.Errorf("StringArray.Value: %w", err)
	}
	return string(b), nil
}

func (a *StringArray) Scan(src interface{}) error {
	if src == nil {
		*a = []string{}
		return nil
	}
	var data []byte
	switch v := src.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("StringArray.Scan: unsupported type %T", src)
	}
	return json.Unmarshal(data, a)
}

type JSONRaw json.RawMessage

func (j JSONRaw) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "[]", nil
	}
	return string(j), nil
}

func (j *JSONRaw) Scan(src interface{}) error {
	if src == nil {
		*j = JSONRaw("[]")
		return nil
	}
	switch v := src.(type) {
	case []byte:
		*j = JSONRaw(v)
	case string:
		*j = JSONRaw(v)
	default:
		return fmt.Errorf("JSONRaw.Scan: unsupported type %T", src)
	}
	return nil
}
