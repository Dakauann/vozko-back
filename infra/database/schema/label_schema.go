package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Label struct {
	ID          string         `gorm:"primaryKey;type:text"`
	WorkspaceID string         `gorm:"type:uuid;not null;index:idx_label_workspace;index:idx_label_ws_del,priority:1"`
	Name        string         `gorm:"not null;size:100"`
	Color       string         `gorm:"size:20"`
	Position    int            `gorm:"default:0"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt `gorm:"index;index:idx_label_ws_del,priority:2"`
}

func (Label) TableName() string {
	return "labels"
}

func (l *Label) BeforeCreate(tx *gorm.DB) error {
	if l.ID == "" {
		l.ID = uuid.New().String()
	}
	return nil
}

type EntryLabel struct {
	ID          string         `gorm:"primaryKey;type:text"`
	LabelID     string         `gorm:"type:uuid;not null;index:idx_entry_label_label;index:idx_el_label_entry_ws,priority:1"`
	EntryID     string         `gorm:"type:uuid;not null;index:idx_entry_label_entry;index:idx_el_entry_type_ws_del,priority:1;index:idx_el_label_entry_ws,priority:2"`
	EntryType   string         `gorm:"type:varchar(20);not null;index:idx_entry_label_entry;index:idx_el_entry_type_ws_del,priority:2;index:idx_el_label_entry_ws,priority:3"`
	WorkspaceID string         `gorm:"type:uuid;not null;index:idx_entry_label_workspace;index:idx_el_entry_type_ws_del,priority:3;index:idx_el_label_entry_ws,priority:4"`
	Label       Label          `gorm:"foreignKey:LabelID"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	DeletedAt   gorm.DeletedAt `gorm:"index;index:idx_el_entry_type_ws_del,priority:4"`
}

func (EntryLabel) TableName() string {
	return "entry_labels"
}

func (el *EntryLabel) BeforeCreate(tx *gorm.DB) error {
	if el.ID == "" {
		el.ID = uuid.New().String()
	}
	return nil
}
