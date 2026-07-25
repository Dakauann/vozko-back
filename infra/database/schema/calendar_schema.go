package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type GoogleCalendarConnection struct {
	ID           string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string         `gorm:"type:uuid;not null"`
	Email        string         `gorm:"not null;size:255"`
	AccessToken  string         `gorm:"not null;type:text"`
	RefreshToken string         `gorm:"not null;type:text"`
	TokenExpiry  time.Time      `gorm:"not null"`
	SyncToken    string         `gorm:"type:text"`
	CreatedAt    time.Time      `gorm:"autoCreateTime"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime"`
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

func (GoogleCalendarConnection) TableName() string { return "google_calendar_connections" }

func (c *GoogleCalendarConnection) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type CalendarEvent struct {
	ID            string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string    `gorm:"type:uuid;not null;index:idx_cal_event_ws_user"`
	UserID        string    `gorm:"type:uuid;not null;index:idx_cal_event_ws_user"`
	GoogleEventID string    `gorm:"size:255;index"`
	Title         string    `gorm:"not null;size:500"`
	Description   string    `gorm:"type:text"`
	Location      string    `gorm:"size:500"`
	StartTime     time.Time `gorm:"not null;index:idx_cal_event_time"`
	EndTime       time.Time `gorm:"not null;index:idx_cal_event_time"`
	AllDay        bool      `gorm:"not null;default:false"`
	TimeZone      string    `gorm:"size:100"`
	Color         string    `gorm:"size:50"`
	Attendees     string    `gorm:"type:text"`
	MeetingLink   string    `gorm:"size:1000"`
	Status        string    `gorm:"size:50;default:'confirmed'"`

	GuestsCanModify         bool `gorm:"not null;default:false"`
	GuestsCanInviteOthers   bool `gorm:"not null;default:true"`
	GuestsCanSeeOtherGuests bool `gorm:"not null;default:true"`

	Visibility   string `gorm:"size:50;default:'default'"`
	Transparency string `gorm:"size:50;default:'opaque'"`

	Recurrence          string `gorm:"type:text"`
	RemindersUseDefault bool   `gorm:"not null;default:true"`
	ReminderOverrides   string `gorm:"type:text"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (CalendarEvent) TableName() string { return "calendar_events" }

func (e *CalendarEvent) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	return nil
}

type CalendarWatchChannel struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID string         `gorm:"type:uuid;not null;uniqueIndex:idx_watch_channel_ws"`
	ChannelID   string         `gorm:"type:uuid;not null;uniqueIndex:idx_watch_channel_cid"`
	ResourceID  string         `gorm:"not null;size:255"`
	Token       string         `gorm:"not null;type:text"`
	Expiration  time.Time      `gorm:"not null;index:idx_watch_channel_exp"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (CalendarWatchChannel) TableName() string { return "calendar_watch_channels" }

func (c *CalendarWatchChannel) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}
