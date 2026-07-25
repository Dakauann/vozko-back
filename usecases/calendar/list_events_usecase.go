package calendar_usecase

import (
	"sort"
	"strings"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/shared"
)

type listEventsUseCase struct {
	repo   calendar.Repository
	google calendar.GoogleOAuthService
}

func NewListEventsUseCase(repo calendar.Repository, google calendar.GoogleOAuthService) calendar.ListEventsUseCase {
	return &listEventsUseCase{repo: repo, google: google}
}

func (uc *listEventsUseCase) Execute(input calendar.ListEventsInput) (*shared.PaginatedResult[*calendar.CalendarEvent], error) {

	conn, err := uc.repo.GetConnection(input.WorkspaceID)
	if err != nil || conn == nil {

		return uc.repo.ListEvents(input)
	}

	accessToken, err := ensureValidToken(uc.google, uc.repo, conn)
	if err != nil {

		return uc.repo.ListEvents(input)
	}

	var timeMin, timeMax time.Time
	if input.From != nil {
		timeMin = *input.From
	}
	if input.To != nil {
		timeMax = *input.To
	}

	pagination := shared.NormalizePagination(input.Pagination)
	maxResults := pagination.PageSize * pagination.Page
	if maxResults < 50 {
		maxResults = 50
	}
	if maxResults > 250 {
		maxResults = 250
	}

	events, err := uc.google.ListGoogleEvents(accessToken, timeMin, timeMax, input.Search, maxResults)
	if err != nil {

		return uc.repo.ListEvents(input)
	}

	if input.Search != "" {
		searchLower := strings.ToLower(input.Search)
		filtered := make([]*calendar.CalendarEvent, 0, len(events))
		for _, ev := range events {
			if strings.Contains(strings.ToLower(ev.Title), searchLower) ||
				strings.Contains(strings.ToLower(ev.Description), searchLower) {
				filtered = append(filtered, ev)
			}
		}
		events = filtered
	}

	for _, ev := range events {
		ev.WorkspaceID = input.WorkspaceID
		ev.UserID = input.UserID

		if ev.ID == "" {
			ev.ID = clientGoogleCalendarEventID(ev.GoogleEventID)
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].StartTime.Before(events[j].StartTime)
	})

	total := int64(len(events))
	start := pagination.Offset()
	end := start + pagination.PageSize
	if start > len(events) {
		start = len(events)
	}
	if end > len(events) {
		end = len(events)
	}
	page := events[start:end]

	return shared.NewPaginatedResult(page, pagination, total), nil
}
