package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Stage struct {
	ID           string         `gorm:"primaryKey;type:text"`
	WorkspaceID  string         `gorm:"type:uuid;not null;index:idx_tag_workspace;index:idx_tag_ws_del,priority:1"`
	CampaignID   string         `gorm:"type:text;default:null;index:idx_tag_campaign"`
	CampaignType string         `gorm:"type:varchar(20);default:null"`
	PipelineID   string         `gorm:"type:uuid;default:null;index:idx_stage_pipeline"`
	Name         string         `gorm:"not null;size:100"`
	Description  string         `gorm:"size:500;default:null"`
	Color        string         `gorm:"size:20"`
	IsDefault    bool           `gorm:"default:false"`
	IsInitial    bool           `gorm:"default:false"`
	IsWon        bool           `gorm:"default:false"`
	IsLost       bool           `gorm:"default:false"`
	Probability  *int           `gorm:"default:null"`
	RotDays      *int           `gorm:"default:null"`
	DefaultKey   string         `gorm:"size:100;default:null"`
	Position     int            `gorm:"default:0"`
	CreatedAt    time.Time      `gorm:"autoCreateTime"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime"`
	DeletedAt    gorm.DeletedAt `gorm:"index;index:idx_tag_ws_del,priority:2"`
}

func (Stage) TableName() string {
	return "stages"
}

func (t *Stage) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}

type EntryStage struct {
	ID          string         `gorm:"primaryKey;type:text"`
	StageID     string         `gorm:"type:uuid;not null;index:idx_entry_tag_tag;index:idx_et_ws_del_tag,priority:3;index:idx_et_tag_ws_del_entry,priority:1"`
	EntryID     string         `gorm:"type:uuid;not null;index:idx_entry_tag_entry;index:idx_et_entry_type_ws,priority:1;index:idx_et_tag_ws_del_entry,priority:4"`
	EntryType   string         `gorm:"type:varchar(20);not null;index:idx_entry_tag_entry;index:idx_et_entry_type_ws,priority:2"`
	WorkspaceID string         `gorm:"type:uuid;not null;index:idx_entry_tag_workspace;index:idx_et_ws_del_tag,priority:1;index:idx_et_entry_type_ws,priority:3;index:idx_et_tag_ws_del_entry,priority:2"`
	Stage       Stage          `gorm:"foreignKey:StageID"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	DeletedAt   gorm.DeletedAt `gorm:"index;index:idx_et_ws_del_tag,priority:2;index:idx_et_tag_ws_del_entry,priority:3"`
}

func (EntryStage) TableName() string {
	return "entry_stages"
}

func (et *EntryStage) BeforeCreate(tx *gorm.DB) error {
	if et.ID == "" {
		et.ID = uuid.New().String()
	}
	return nil
}
