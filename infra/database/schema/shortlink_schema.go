package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ShortLink struct {
	ID               string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID      string         `gorm:"type:uuid;not null;index"`
	DepartmentID     *string        `gorm:"type:uuid;index"`
	CreatedBy        *string        `gorm:"type:uuid"`
	Code             string         `gorm:"type:varchar(32);not null;index"`
	TargetURL        string         `gorm:"type:text;not null"`
	Title            string         `gorm:"type:varchar(200)"`
	RedirectType     string         `gorm:"not null;size:8;default:'302'"`
	Status           string         `gorm:"not null;size:16;default:'active'"`
	PasswordHash     string         `gorm:"type:text"`
	ExpiresAt        *time.Time     `gorm:"index"`
	MaxClicks        *int64         `gorm:"type:bigint"`
	ClickCount       int64          `gorm:"not null;default:0"`
	UniqueClickCount int64          `gorm:"not null;default:0"`
	LastClickedAt    *time.Time     `gorm:""`
	CreatedAt        time.Time      `gorm:"autoCreateTime"`
	UpdatedAt        time.Time      `gorm:"autoUpdateTime"`
	DeletedAt        gorm.DeletedAt `gorm:"index"`
}

func (ShortLink) TableName() string {
	return "short_links"
}

func (l *ShortLink) BeforeCreate(tx *gorm.DB) error {
	if l.ID == "" {
		l.ID = uuid.New().String()
	}
	return nil
}

type ShortLinkClick struct {
	ID            string    `gorm:"primaryKey;type:uuid"`
	ShortLinkID   string    `gorm:"type:uuid;not null;index:idx_short_link_clicks_link"`
	WorkspaceID   string    `gorm:"type:uuid;not null;index"`
	OccurredAt    time.Time `gorm:"not null;index:idx_short_link_clicks_link"`
	IPHash        string    `gorm:"type:varchar(64)"`
	Country       string    `gorm:"type:varchar(80)"`
	Region        string    `gorm:"type:varchar(120)"`
	City          string    `gorm:"type:varchar(120)"`
	DeviceType    string    `gorm:"type:varchar(20)"`
	OS            string    `gorm:"type:varchar(40)"`
	Browser       string    `gorm:"type:varchar(40)"`
	RefererDomain string    `gorm:"type:varchar(255)"`
	UTMSource     string    `gorm:"type:varchar(120)"`
	UTMMedium     string    `gorm:"type:varchar(120)"`
	UTMCampaign   string    `gorm:"type:varchar(120)"`
	IsBot         bool      `gorm:"not null;default:false"`
	IsProxy       bool      `gorm:"not null;default:false"`
	Language      string    `gorm:"type:varchar(20)"`
	CreatedAt     time.Time `gorm:"autoCreateTime"`
}

func (ShortLinkClick) TableName() string {
	return "short_link_clicks"
}

func (c *ShortLinkClick) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type ShortLinkDailyStat struct {
	ID             string    `gorm:"primaryKey;type:uuid"`
	ShortLinkID    string    `gorm:"type:uuid;not null;uniqueIndex:idx_sl_daily_unique,priority:1;index:idx_sl_daily_link,priority:1"`
	WorkspaceID    string    `gorm:"type:uuid;not null;index"`
	Day            time.Time `gorm:"type:date;not null;uniqueIndex:idx_sl_daily_unique,priority:2;index:idx_sl_daily_link,priority:2"`
	Dimension      string    `gorm:"type:varchar(20);not null;uniqueIndex:idx_sl_daily_unique,priority:3"`
	DimensionValue string    `gorm:"type:varchar(255);not null;default:'';uniqueIndex:idx_sl_daily_unique,priority:4"`
	Clicks         int64     `gorm:"not null;default:0"`
	UniqueClicks   int64     `gorm:"not null;default:0"`
	CreatedAt      time.Time `gorm:"autoCreateTime"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime"`
}

func (ShortLinkDailyStat) TableName() string {
	return "short_link_daily_stats"
}

func (s *ShortLinkDailyStat) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}
