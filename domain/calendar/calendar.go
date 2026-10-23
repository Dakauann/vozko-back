package calendar

import (
	"context"
	"errors"
	"time"

	"vozko/domain/googleaccount"
	"vozko/domain/shared"
)

var (
	ErrEventNotFound        = errors.New("calendar event not found")
	ErrGoogleNotConnected   = errors.New("google calendar not connected for this workspace")
	ErrInvalidTimeRange     = errors.New("event end time must be after start time")
	ErrTitleRequired        = errors.New("event title is required")
	ErrWatchChannelNotFound = errors.New("calendar watch channel not found")
	ErrSlotConflict         = errors.New("the requested time slot is already occupied")
)

type AccessProvider interface {
	CalendarAccess(ctx context.Context, workspaceID string) (googleaccount.Access, error)
}

type LegacyGoogleConnection struct {
	ID           string
	WorkspaceID  string
	Email        string
	AccessToken  string
	RefreshToken string
	TokenExpiry  time.Time
	SyncToken    string
}

type LegacyConnectionStore interface {
	ListLegacyConnections(ctx context.Context) ([]LegacyGoogleConnection, error)
	RetireLegacyConnection(ctx context.Context, id string) error
}

type CalendarWatchChannel struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	ChannelID   string    `json:"channelId"`
	ResourceID  string    `json:"resourceId"`
	Token       string    `json:"-"`
	Expiration  time.Time `json:"expiration"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type CalendarEvent struct {
	ID            string     `json:"id"`
	WorkspaceID   string     `json:"workspaceId"`
	UserID        string     `json:"userId"`
	GoogleEventID string     `json:"googleEventId,omitempty"`
	Title         string     `json:"title"`
	Description   string     `json:"description,omitempty"`
	Location      string     `json:"location,omitempty"`
	StartTime     time.Time  `json:"startTime"`
	EndTime       time.Time  `json:"endTime"`
	AllDay        bool       `json:"allDay"`
	TimeZone      string     `json:"timeZone,omitempty"`
	Color         string     `json:"color,omitempty"`
	Attendees     []Attendee `json:"attendees,omitempty"`
	MeetingLink   string     `json:"meetingLink,omitempty"`
	Status        string     `json:"status,omitempty"`

	GuestsCanModify         bool `json:"guestsCanModify"`
	GuestsCanInviteOthers   bool `json:"guestsCanInviteOthers"`
	GuestsCanSeeOtherGuests bool `json:"guestsCanSeeOtherGuests"`

	Visibility   string `json:"visibility,omitempty"`
	Transparency string `json:"transparency,omitempty"`

	Recurrence []string `json:"recurrence,omitempty"`

	RemindersUseDefault bool               `json:"remindersUseDefault"`
	ReminderOverrides   []ReminderOverride `json:"reminderOverrides,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Attendee struct {
	Email    string `json:"email"`
	Name     string `json:"name,omitempty"`
	Status   string `json:"status,omitempty"`
	Optional bool   `json:"optional,omitempty"`
}

type ReminderOverride struct {
	Method  string `json:"method"`
	Minutes int    `json:"minutes"`
}

type CreateEventInput struct {
	WorkspaceID      string
	UserID           string
	Title            string
	Description      string
	Location         string
	StartTime        time.Time
	EndTime          time.Time
	AllDay           bool
	TimeZone         string
	Color            string
	Attendees        []Attendee
	MeetingLink      string
	CreateGoogleMeet bool

	GuestsCanModify         bool
	GuestsCanInviteOthers   *bool
	GuestsCanSeeOtherGuests *bool
	Visibility              string
	Transparency            string
	Recurrence              []string
	RemindersUseDefault     bool
	ReminderOverrides       []ReminderOverride
	SendUpdates             string
}

type UpdateEventInput struct {
	EventID     string
	WorkspaceID string
	UserID      string
	Title       *string
	Description *string
	Location    *string
	StartTime   *time.Time
	EndTime     *time.Time
	AllDay      *bool
	TimeZone    *string
	Color       *string
	Attendees   []Attendee
	MeetingLink *string

	GuestsCanModify         *bool
	GuestsCanInviteOthers   *bool
	GuestsCanSeeOtherGuests *bool
	Visibility              *string
	Transparency            *string
	Recurrence              []string
	RemindersUseDefault     *bool
	ReminderOverrides       []ReminderOverride
	SendUpdates             string
	CheckConflict           bool
}

type RescheduleEventInput struct {
	EventID           string
	WorkspaceID       string
	UserID            string
	NewStartTime      time.Time
	NewEndTime        *time.Time
	DurationMinutes   int
	SendUpdates       string
	SkipConflictCheck bool
}

type ListEventsInput struct {
	WorkspaceID string
	UserID      string
	From        *time.Time
	To          *time.Time
	Search      string
	Pagination  shared.Pagination
}

type Repository interface {
	CreateEvent(event *CalendarEvent) error
	UpdateEvent(eventID string, event *CalendarEvent) error
	DeleteEvent(eventID, workspaceID string) error
	GetEvent(eventID, workspaceID string) (*CalendarEvent, error)
	ListEvents(input ListEventsInput) (*shared.PaginatedResult[*CalendarEvent], error)

	GetSyncToken(workspaceID string) (string, error)
	SaveSyncToken(workspaceID, syncToken string) error

	SaveWatchChannel(ch *CalendarWatchChannel) error
	GetWatchChannelByChannelID(channelID string) (*CalendarWatchChannel, error)
	GetWatchChannelByWorkspace(workspaceID string) (*CalendarWatchChannel, error)
	DeleteWatchChannel(id string) error
	ListExpiringWatchChannels(before time.Time) ([]*CalendarWatchChannel, error)

	GetEventByGoogleEventID(googleEventID, workspaceID string) (*CalendarEvent, error)
	UpdateEventStatus(eventID, workspaceID, status string) error
}

type CreateEventUseCase interface {
	Execute(input CreateEventInput) (*CalendarEvent, error)
}

type UpdateEventUseCase interface {
	Execute(input UpdateEventInput) (*CalendarEvent, error)
}

type RescheduleEventUseCase interface {
	Execute(input RescheduleEventInput) (*CalendarEvent, error)
}

type DeleteEventUseCase interface {
	Execute(eventID, workspaceID, userID string) error
}

type GetEventUseCase interface {
	Execute(eventID, workspaceID string) (*CalendarEvent, error)
}

type ListEventsUseCase interface {
	Execute(input ListEventsInput) (*shared.PaginatedResult[*CalendarEvent], error)
}

type StartWatchUseCase interface {
	Execute(workspaceID string) (*CalendarWatchChannel, error)
}

type HandleNotificationUseCase interface {
	Execute(channelID, resourceID, token, resourceState string) error
}

type StopWatchUseCase interface {
	Execute(workspaceID string) error
}

type RenewExpiringChannelsUseCase interface {
	Execute() (renewed int, err error)
}

type GoogleCalendarAPI interface {
	CreateGoogleEvent(accessToken string, event *CalendarEvent, createMeet bool, sendUpdates string) (googleEventID string, meetLink string, err error)
	GetGoogleEvent(accessToken string, googleEventID string) (*CalendarEvent, error)
	UpdateGoogleEvent(accessToken string, googleEventID string, event *CalendarEvent, sendUpdates string) error
	DeleteGoogleEvent(accessToken string, googleEventID string, sendUpdates string) error
	ListGoogleEvents(accessToken string, timeMin, timeMax time.Time, query string, maxResults int) ([]*CalendarEvent, error)

	WatchEvents(accessToken, channelID, token, webhookURL string) (*WatchResponse, error)
	StopChannel(accessToken, channelID, resourceID string) error
	ListEventsIncremental(accessToken, syncToken string) (*IncrementalSyncResult, error)
}

type WatchResponse struct {
	ChannelID  string
	ResourceID string
	Expiration time.Time
}

type IncrementalSyncResult struct {
	Events        []*CalendarEvent
	NextSyncToken string
	SyncExpired   bool
}
