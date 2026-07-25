package calendar

import (
	"time"

	"vozko/delivery/http/response"
	calendardomain "vozko/domain/calendar"
)

type CreateEventRequest struct {
	Title            string                    `json:"title" example:"Reunião com cliente"`
	Description      string                    `json:"description" example:"Alinhamento de proposta comercial"`
	Location         string                    `json:"location" example:"Escritório central"`
	StartTime        string                    `json:"startTime" example:"2026-07-20T14:00:00-03:00"`
	EndTime          string                    `json:"endTime" example:"2026-07-20T15:00:00-03:00"`
	AllDay           bool                      `json:"allDay" example:"false"`
	TimeZone         string                    `json:"timeZone" example:"America/Sao_Paulo"`
	Color            string                    `json:"color" example:"#2463eb"`
	Attendees        []calendardomain.Attendee `json:"attendees" swaggertype:"array,object"`
	MeetingLink      string                    `json:"meetingLink" example:"https://meet.google.com/abc-defg-hij"`
	CreateGoogleMeet bool                      `json:"createGoogleMeet" example:"true"`

	GuestsCanModify         bool                              `json:"guestsCanModify" example:"false"`
	GuestsCanInviteOthers   *bool                             `json:"guestsCanInviteOthers"`
	GuestsCanSeeOtherGuests *bool                             `json:"guestsCanSeeOtherGuests"`
	Visibility              string                            `json:"visibility" example:"default"`
	Transparency            string                            `json:"transparency" example:"opaque"`
	Recurrence              []string                          `json:"recurrence"`
	RemindersUseDefault     bool                              `json:"remindersUseDefault" example:"true"`
	ReminderOverrides       []calendardomain.ReminderOverride `json:"reminderOverrides" swaggertype:"array,object"`
	SendUpdates             string                            `json:"sendUpdates" example:"all"`
}

type UpdateEventRequest struct {
	Title       *string                   `json:"title" example:"Reunião com cliente"`
	Description *string                   `json:"description" example:"Alinhamento de proposta comercial"`
	Location    *string                   `json:"location" example:"Escritório central"`
	StartTime   *string                   `json:"startTime" example:"2026-07-20T14:00:00-03:00"`
	EndTime     *string                   `json:"endTime" example:"2026-07-20T15:00:00-03:00"`
	AllDay      *bool                     `json:"allDay"`
	TimeZone    *string                   `json:"timeZone" example:"America/Sao_Paulo"`
	Color       *string                   `json:"color" example:"#2463eb"`
	Attendees   []calendardomain.Attendee `json:"attendees" swaggertype:"array,object"`
	MeetingLink *string                   `json:"meetingLink" example:"https://meet.google.com/abc-defg-hij"`

	GuestsCanModify         *bool                             `json:"guestsCanModify"`
	GuestsCanInviteOthers   *bool                             `json:"guestsCanInviteOthers"`
	GuestsCanSeeOtherGuests *bool                             `json:"guestsCanSeeOtherGuests"`
	Visibility              *string                           `json:"visibility" example:"default"`
	Transparency            *string                           `json:"transparency" example:"opaque"`
	Recurrence              []string                          `json:"recurrence"`
	RemindersUseDefault     *bool                             `json:"remindersUseDefault"`
	ReminderOverrides       []calendardomain.ReminderOverride `json:"reminderOverrides" swaggertype:"array,object"`
	SendUpdates             string                            `json:"sendUpdates" example:"all"`
}

type ConnectGoogleRequest struct {
	Code        string `json:"code" example:"4/0AeanS0b..."`
	RedirectURI string `json:"redirectUri" example:"https://app.example.com/dashboard/integrations"`
}

type AttendeeResponse struct {
	Email    string `json:"email" example:"cliente@empresa.com"`
	Name     string `json:"name,omitempty" example:"João Silva"`
	Status   string `json:"status,omitempty" example:"accepted"`
	Optional bool   `json:"optional,omitempty" example:"false"`
}

type ReminderOverrideResponse struct {
	Method  string `json:"method" example:"popup"`
	Minutes int    `json:"minutes" example:"30"`
}

type EventResponse struct {
	ID            string             `json:"id" example:"evt_a1b2c3"`
	WorkspaceID   string             `json:"workspaceId" example:"ws_a1b2c3"`
	UserID        string             `json:"userId" example:"usr_a1b2c3"`
	GoogleEventID string             `json:"googleEventId,omitempty" example:"g_a1b2c3"`
	Title         string             `json:"title" example:"Reunião com cliente"`
	Description   string             `json:"description,omitempty" example:"Alinhamento de proposta comercial"`
	Location      string             `json:"location,omitempty" example:"Escritório central"`
	StartTime     time.Time          `json:"startTime"`
	EndTime       time.Time          `json:"endTime"`
	AllDay        bool               `json:"allDay" example:"false"`
	TimeZone      string             `json:"timeZone,omitempty" example:"America/Sao_Paulo"`
	Color         string             `json:"color,omitempty" example:"#2463eb"`
	Attendees     []AttendeeResponse `json:"attendees,omitempty"`
	MeetingLink   string             `json:"meetingLink,omitempty" example:"https://meet.google.com/abc-defg-hij"`
	Status        string             `json:"status,omitempty" example:"confirmed"`

	GuestsCanModify         bool `json:"guestsCanModify" example:"false"`
	GuestsCanInviteOthers   bool `json:"guestsCanInviteOthers" example:"true"`
	GuestsCanSeeOtherGuests bool `json:"guestsCanSeeOtherGuests" example:"true"`

	Visibility   string `json:"visibility,omitempty" example:"default"`
	Transparency string `json:"transparency,omitempty" example:"opaque"`

	Recurrence []string `json:"recurrence,omitempty"`

	RemindersUseDefault bool                       `json:"remindersUseDefault" example:"true"`
	ReminderOverrides   []ReminderOverrideResponse `json:"reminderOverrides,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ConnectionResponse struct {
	ID          string    `json:"id" example:"gcc_a1b2c3"`
	WorkspaceID string    `json:"workspaceId" example:"ws_a1b2c3"`
	Email       string    `json:"email" example:"agenda@empresa.com"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type WatchChannelResponse struct {
	ChannelID  string    `json:"channelId" example:"chn_a1b2c3"`
	Expiration time.Time `json:"expiration"`
}

type EventEnvelope struct {
	Event *EventResponse `json:"event"`
}

type EventListResponse struct {
	Data []*EventResponse        `json:"data"`
	Meta response.PaginationMeta `json:"meta"`
}

type ConnectionEnvelope struct {
	Connection *ConnectionResponse `json:"connection"`
}

type ConnectionStatusResponse struct {
	Connected  bool                `json:"connected" example:"true"`
	Connection *ConnectionResponse `json:"connection"`
}

type WatchEnvelope struct {
	Channel WatchChannelResponse `json:"channel"`
}

type AuthURLResponse struct {
	AuthURL string `json:"authUrl" example:"https://accounts.google.com/o/oauth2/v2/auth?client_id=..."`
}

type DeletedResponse struct {
	Deleted bool `json:"deleted" example:"true"`
}

type DisconnectedResponse struct {
	Disconnected bool `json:"disconnected" example:"true"`
}

type StoppedResponse struct {
	Stopped bool `json:"stopped" example:"true"`
}

func toAttendeeResponses(items []calendardomain.Attendee) []AttendeeResponse {
	if items == nil {
		return nil
	}
	out := make([]AttendeeResponse, len(items))
	for i, a := range items {
		out[i] = AttendeeResponse{
			Email:    a.Email,
			Name:     a.Name,
			Status:   a.Status,
			Optional: a.Optional,
		}
	}
	return out
}

func toReminderOverrideResponses(items []calendardomain.ReminderOverride) []ReminderOverrideResponse {
	if items == nil {
		return nil
	}
	out := make([]ReminderOverrideResponse, len(items))
	for i, o := range items {
		out[i] = ReminderOverrideResponse{
			Method:  o.Method,
			Minutes: o.Minutes,
		}
	}
	return out
}

func toEventResponse(e *calendardomain.CalendarEvent) *EventResponse {
	if e == nil {
		return nil
	}
	return &EventResponse{
		ID:                      e.ID,
		WorkspaceID:             e.WorkspaceID,
		UserID:                  e.UserID,
		GoogleEventID:           e.GoogleEventID,
		Title:                   e.Title,
		Description:             e.Description,
		Location:                e.Location,
		StartTime:               e.StartTime,
		EndTime:                 e.EndTime,
		AllDay:                  e.AllDay,
		TimeZone:                e.TimeZone,
		Color:                   e.Color,
		Attendees:               toAttendeeResponses(e.Attendees),
		MeetingLink:             e.MeetingLink,
		Status:                  e.Status,
		GuestsCanModify:         e.GuestsCanModify,
		GuestsCanInviteOthers:   e.GuestsCanInviteOthers,
		GuestsCanSeeOtherGuests: e.GuestsCanSeeOtherGuests,
		Visibility:              e.Visibility,
		Transparency:            e.Transparency,
		Recurrence:              e.Recurrence,
		RemindersUseDefault:     e.RemindersUseDefault,
		ReminderOverrides:       toReminderOverrideResponses(e.ReminderOverrides),
		CreatedAt:               e.CreatedAt,
		UpdatedAt:               e.UpdatedAt,
	}
}

func toEventResponses(items []*calendardomain.CalendarEvent) []*EventResponse {
	if items == nil {
		return nil
	}
	out := make([]*EventResponse, len(items))
	for i, it := range items {
		out[i] = toEventResponse(it)
	}
	return out
}

func toConnectionResponse(c *calendardomain.GoogleCalendarConnection) *ConnectionResponse {
	if c == nil {
		return nil
	}
	return &ConnectionResponse{
		ID:          c.ID,
		WorkspaceID: c.WorkspaceID,
		Email:       c.Email,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}
