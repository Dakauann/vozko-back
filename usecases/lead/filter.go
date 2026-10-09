package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadarea"
)

type WorkspaceZones interface {
	Location(ctx context.Context, workspaceID, departmentID string) (*time.Location, error)
}

func bindLeadFilter(v lead.Viewer, f crmfilter.Filter) (crmfilter.Filter, error) {
	if err := lead.CheckAddressFilter(f, v); err != nil {
		return crmfilter.Filter{}, err
	}
	bound, err := customfield.BindFilter(f, v.Definitions, v.Fields)
	if err == nil {
		return bound, nil
	}
	if errors.Is(err, customfield.ErrFilterSensitive) {
		return crmfilter.Filter{}, err
	}
	return crmfilter.Filter{}, fmt.Errorf("%w: %w", lead.ErrLeadFilterInvalid, err)
}

type filterBinding struct {
	areas AreaFilters
	zones WorkspaceZones
	now   func() time.Time
}

type boundFilter struct {
	filter crmfilter.Filter
	stamps leadarea.Stamps
	today  time.Time
}

func (b filterBinding) bind(ctx context.Context, a Actor, v lead.Viewer, f crmfilter.Filter) (boundFilter, error) {
	bound, err := bindLeadFilter(v, f)
	if err != nil {
		return boundFilter{}, err
	}
	bound, stamps, err := bindLeadAreas(ctx, b.areas, a, bound)
	if err != nil {
		return boundFilter{}, err
	}
	out := boundFilter{filter: bound, stamps: stamps}
	if bound.UsesField(crmfilter.FieldBirthday) {
		if out.today, err = b.today(ctx, a.WorkspaceID); err != nil {
			return boundFilter{}, err
		}
	}
	return out, nil
}

func (b filterBinding) today(ctx context.Context, workspaceID string) (time.Time, error) {
	return workspaceToday(ctx, b.zones, clockOr(b.now), workspaceID)
}

func workspaceToday(ctx context.Context, zones WorkspaceZones, now func() time.Time, workspaceID string) (time.Time, error) {
	loc, err := zones.Location(ctx, workspaceID, "")
	if err != nil {
		return time.Time{}, fmt.Errorf("time zone of workspace %s: %w", workspaceID, err)
	}
	if loc == nil {
		loc = time.UTC
	}
	return now().In(loc), nil
}

func clockOr(now func() time.Time) func() time.Time {
	if now == nil {
		return time.Now
	}
	return now
}
