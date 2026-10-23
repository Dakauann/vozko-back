package calendar_repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/calendar"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) calendar.Repository {
	return &repository{db: db}
}

func (r *repository) CreateEvent(event *calendar.CalendarEvent) error {
	dbEvent := mapEventToSchema(event)
	if err := r.db.Create(dbEvent).Error; err != nil {
		return err
	}
	event.ID = dbEvent.ID
	event.CreatedAt = dbEvent.CreatedAt
	event.UpdatedAt = dbEvent.UpdatedAt
	return nil
}

func (r *repository) UpdateEvent(eventID string, event *calendar.CalendarEvent) error {
	update := map[string]interface{}{
		"title":                       event.Title,
		"description":                 event.Description,
		"location":                    event.Location,
		"start_time":                  event.StartTime,
		"end_time":                    event.EndTime,
		"all_day":                     event.AllDay,
		"time_zone":                   event.TimeZone,
		"color":                       event.Color,
		"attendees":                   encodeAttendees(event.Attendees),
		"meeting_link":                event.MeetingLink,
		"status":                      event.Status,
		"google_event_id":             event.GoogleEventID,
		"guests_can_modify":           event.GuestsCanModify,
		"guests_can_invite_others":    event.GuestsCanInviteOthers,
		"guests_can_see_other_guests": event.GuestsCanSeeOtherGuests,
		"visibility":                  event.Visibility,
		"transparency":                event.Transparency,
		"recurrence":                  encodeStringSlice(event.Recurrence),
		"reminders_use_default":       event.RemindersUseDefault,
		"reminder_overrides":          encodeReminderOverrides(event.ReminderOverrides),
	}

	result := r.db.Model(&schema.CalendarEvent{}).Where("id = ? AND workspace_id = ?", eventID, event.WorkspaceID).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return calendar.ErrEventNotFound
	}
	return nil
}

func (r *repository) DeleteEvent(eventID, workspaceID string) error {
	result := r.db.Where("id = ? AND workspace_id = ?", eventID, workspaceID).Delete(&schema.CalendarEvent{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return calendar.ErrEventNotFound
	}
	return nil
}

func (r *repository) GetEvent(eventID, workspaceID string) (*calendar.CalendarEvent, error) {
	var dbEvent schema.CalendarEvent
	if err := r.db.Where("id = ? AND workspace_id = ?", eventID, workspaceID).First(&dbEvent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, calendar.ErrEventNotFound
		}
		return nil, err
	}
	return mapEventToDomain(&dbEvent), nil
}

func (r *repository) ListEvents(input calendar.ListEventsInput) (*shared.PaginatedResult[*calendar.CalendarEvent], error) {
	pagination := shared.NormalizePagination(input.Pagination)
	query := r.db.Model(&schema.CalendarEvent{}).Where("workspace_id = ?", input.WorkspaceID)

	if input.UserID != "" {
		query = query.Where("user_id = ?", input.UserID)
	}

	if input.From != nil {
		query = query.Where("end_time >= ?", *input.From)
	}
	if input.To != nil {
		query = query.Where("start_time <= ?", *input.To)
	}

	if input.Search != "" {
		searchPattern := "%" + strings.ToLower(input.Search) + "%"
		query = query.Where("LOWER(title) LIKE ? OR LOWER(description) LIKE ?", searchPattern, searchPattern)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var dbEvents []schema.CalendarEvent
	if err := query.
		Order("start_time ASC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&dbEvents).Error; err != nil {
		return nil, err
	}

	events := make([]*calendar.CalendarEvent, len(dbEvents))
	for i := range dbEvents {
		events[i] = mapEventToDomain(&dbEvents[i])
	}

	return shared.NewPaginatedResult(events, pagination, total), nil
}

func mapEventToSchema(event *calendar.CalendarEvent) *schema.CalendarEvent {
	return &schema.CalendarEvent{
		ID:                      event.ID,
		WorkspaceID:             event.WorkspaceID,
		UserID:                  event.UserID,
		GoogleEventID:           event.GoogleEventID,
		Title:                   event.Title,
		Description:             event.Description,
		Location:                event.Location,
		StartTime:               event.StartTime,
		EndTime:                 event.EndTime,
		AllDay:                  event.AllDay,
		TimeZone:                event.TimeZone,
		Color:                   event.Color,
		Attendees:               encodeAttendees(event.Attendees),
		MeetingLink:             event.MeetingLink,
		Status:                  event.Status,
		GuestsCanModify:         event.GuestsCanModify,
		GuestsCanInviteOthers:   event.GuestsCanInviteOthers,
		GuestsCanSeeOtherGuests: event.GuestsCanSeeOtherGuests,
		Visibility:              event.Visibility,
		Transparency:            event.Transparency,
		Recurrence:              encodeStringSlice(event.Recurrence),
		RemindersUseDefault:     event.RemindersUseDefault,
		ReminderOverrides:       encodeReminderOverrides(event.ReminderOverrides),
	}
}

func mapEventToDomain(dbEvent *schema.CalendarEvent) *calendar.CalendarEvent {
	return &calendar.CalendarEvent{
		ID:                      dbEvent.ID,
		WorkspaceID:             dbEvent.WorkspaceID,
		UserID:                  dbEvent.UserID,
		GoogleEventID:           dbEvent.GoogleEventID,
		Title:                   dbEvent.Title,
		Description:             dbEvent.Description,
		Location:                dbEvent.Location,
		StartTime:               dbEvent.StartTime,
		EndTime:                 dbEvent.EndTime,
		AllDay:                  dbEvent.AllDay,
		TimeZone:                dbEvent.TimeZone,
		Color:                   dbEvent.Color,
		Attendees:               decodeAttendees(dbEvent.Attendees),
		MeetingLink:             dbEvent.MeetingLink,
		Status:                  dbEvent.Status,
		GuestsCanModify:         dbEvent.GuestsCanModify,
		GuestsCanInviteOthers:   dbEvent.GuestsCanInviteOthers,
		GuestsCanSeeOtherGuests: dbEvent.GuestsCanSeeOtherGuests,
		Visibility:              dbEvent.Visibility,
		Transparency:            dbEvent.Transparency,
		Recurrence:              decodeStringSlice(dbEvent.Recurrence),
		RemindersUseDefault:     dbEvent.RemindersUseDefault,
		ReminderOverrides:       decodeReminderOverrides(dbEvent.ReminderOverrides),
		CreatedAt:               dbEvent.CreatedAt,
		UpdatedAt:               dbEvent.UpdatedAt,
	}
}

func encodeAttendees(attendees []calendar.Attendee) string {
	if len(attendees) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(attendees)
	return string(data)
}

func decodeAttendees(raw string) []calendar.Attendee {
	if raw == "" || raw == "[]" {
		return nil
	}
	var attendees []calendar.Attendee
	_ = json.Unmarshal([]byte(raw), &attendees)
	return attendees
}

func encodeStringSlice(ss []string) string {
	if len(ss) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(ss)
	return string(data)
}

func decodeStringSlice(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var ss []string
	_ = json.Unmarshal([]byte(raw), &ss)
	return ss
}

func encodeReminderOverrides(overrides []calendar.ReminderOverride) string {
	if len(overrides) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(overrides)
	return string(data)
}

func decodeReminderOverrides(raw string) []calendar.ReminderOverride {
	if raw == "" || raw == "[]" {
		return nil
	}
	var overrides []calendar.ReminderOverride
	_ = json.Unmarshal([]byte(raw), &overrides)
	return overrides
}

func (r *repository) SaveWatchChannel(ch *calendar.CalendarWatchChannel) error {
	dbCh := &schema.CalendarWatchChannel{
		ID:          ch.ID,
		WorkspaceID: ch.WorkspaceID,
		ChannelID:   ch.ChannelID,
		ResourceID:  ch.ResourceID,
		Token:       ch.Token,
		Expiration:  ch.Expiration,
	}
	if err := r.db.Create(dbCh).Error; err != nil {
		return err
	}
	ch.ID = dbCh.ID
	ch.CreatedAt = dbCh.CreatedAt
	ch.UpdatedAt = dbCh.UpdatedAt
	return nil
}

func (r *repository) GetWatchChannelByChannelID(channelID string) (*calendar.CalendarWatchChannel, error) {
	var dbCh schema.CalendarWatchChannel
	if err := r.db.Where("channel_id = ?", channelID).First(&dbCh).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, calendar.ErrWatchChannelNotFound
		}
		return nil, err
	}
	return mapWatchChannelToDomain(&dbCh), nil
}

func (r *repository) GetWatchChannelByWorkspace(workspaceID string) (*calendar.CalendarWatchChannel, error) {
	var dbCh schema.CalendarWatchChannel
	if err := r.db.Where("workspace_id = ?", workspaceID).First(&dbCh).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapWatchChannelToDomain(&dbCh), nil
}

func (r *repository) DeleteWatchChannel(id string) error {
	return r.db.Where("id = ?", id).Delete(&schema.CalendarWatchChannel{}).Error
}

func (r *repository) ListExpiringWatchChannels(before time.Time) ([]*calendar.CalendarWatchChannel, error) {
	var channels []schema.CalendarWatchChannel
	if err := r.db.Where("expiration < ?", before).Find(&channels).Error; err != nil {
		return nil, err
	}
	result := make([]*calendar.CalendarWatchChannel, len(channels))
	for i := range channels {
		result[i] = mapWatchChannelToDomain(&channels[i])
	}
	return result, nil
}

func (r *repository) GetEventByGoogleEventID(googleEventID, workspaceID string) (*calendar.CalendarEvent, error) {
	var dbEvent schema.CalendarEvent
	if err := r.db.Where("google_event_id = ? AND workspace_id = ?", googleEventID, workspaceID).First(&dbEvent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapEventToDomain(&dbEvent), nil
}

func (r *repository) UpdateEventStatus(eventID, workspaceID, status string) error {
	result := r.db.Model(&schema.CalendarEvent{}).
		Where("id = ? AND workspace_id = ?", eventID, workspaceID).
		Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return calendar.ErrEventNotFound
	}
	return nil
}

func mapWatchChannelToDomain(dbCh *schema.CalendarWatchChannel) *calendar.CalendarWatchChannel {
	return &calendar.CalendarWatchChannel{
		ID:          dbCh.ID,
		WorkspaceID: dbCh.WorkspaceID,
		ChannelID:   dbCh.ChannelID,
		ResourceID:  dbCh.ResourceID,
		Token:       dbCh.Token,
		Expiration:  dbCh.Expiration,
		CreatedAt:   dbCh.CreatedAt,
		UpdatedAt:   dbCh.UpdatedAt,
	}
}

func (r *repository) GetSyncToken(workspaceID string) (string, error) {
	var state schema.CalendarSyncState
	if err := r.db.Where("workspace_id = ?", workspaceID).Limit(1).Find(&state).Error; err != nil {
		return "", err
	}
	return state.SyncToken, nil
}

func (r *repository) SaveSyncToken(workspaceID, syncToken string) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workspace_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"sync_token", "updated_at"}),
	}).Create(&schema.CalendarSyncState{WorkspaceID: workspaceID, SyncToken: syncToken}).Error
}

type legacyConnectionStore struct {
	db *gorm.DB
}

func NewLegacyConnectionStore(db *gorm.DB) calendar.LegacyConnectionStore {
	return &legacyConnectionStore{db: db}
}

func (s *legacyConnectionStore) ListLegacyConnections(ctx context.Context) ([]calendar.LegacyGoogleConnection, error) {
	var rows []schema.GoogleCalendarConnection
	if err := s.db.WithContext(ctx).Order("created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]calendar.LegacyGoogleConnection, 0, len(rows))
	for _, row := range rows {
		out = append(out, calendar.LegacyGoogleConnection{
			ID:           row.ID,
			WorkspaceID:  row.WorkspaceID,
			Email:        row.Email,
			AccessToken:  row.AccessToken,
			RefreshToken: row.RefreshToken,
			TokenExpiry:  row.TokenExpiry,
			SyncToken:    row.SyncToken,
		})
	}
	return out, nil
}

func (s *legacyConnectionStore) RetireLegacyConnection(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Model(&schema.GoogleCalendarConnection{}).Where("id = ?", id).Updates(map[string]any{
		"access_token":  "",
		"refresh_token": "",
		"deleted_at":    time.Now().UTC(),
	}).Error
}
