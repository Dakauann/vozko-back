package calendar_usecase

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/calendar"
	"vozko/domain/googleaccount"
)

type accessProvider struct {
	source googleaccount.AccessSource
}

func NewAccessProvider(source googleaccount.AccessSource) calendar.AccessProvider {
	if source == nil {
		panic("calendar: the access provider requires the google account token source")
	}
	return &accessProvider{source: source}
}

func (p *accessProvider) CalendarAccess(ctx context.Context, workspaceID string) (googleaccount.Access, error) {
	access, err := p.source.Access(ctx, workspaceID, googleaccount.ProductCalendar)
	if errors.Is(err, googleaccount.ErrNotConnected) {
		return googleaccount.Access{}, fmt.Errorf("%w: %w", calendar.ErrGoogleNotConnected, err)
	}
	if err != nil {
		return googleaccount.Access{}, fmt.Errorf("google auth: %w", err)
	}
	return access, nil
}

func calendarToken(access calendar.AccessProvider, workspaceID string) (string, error) {
	granted, err := access.CalendarAccess(context.Background(), workspaceID)
	if err != nil {
		return "", err
	}
	return granted.Token, nil
}
