package geocoding

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"vozko/domain/billing"
	"vozko/domain/leadmap"
	"vozko/domain/shared"
)

const (
	HistoryCycles           = 12
	PlatformDefaultPageSize = 20
	PlatformMaxPageSize     = 50
	PlatformSearchMax       = 100
	PlatformMaxPage         = 1_000_000
)

var ErrPlatformForbidden = errors.New("geocoding: only a platform admin can read the geocoding of every workspace")

type Period struct {
	CycleStart time.Time
	NextCycle  time.Time
	Day        time.Time
	NextDay    time.Time
}

func cycleQuota() shared.MonthlyQuota {
	return shared.MonthlyQuota{CycleDay: cycleDay, Location: billing.LocationBRT()}
}

func PeriodAt(now time.Time) Period {
	quota := cycleQuota()
	local := now.In(billing.LocationBRT())
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	return Period{CycleStart: quota.CycleStart(now), NextCycle: quota.NextCycleStart(now), Day: day, NextDay: day.AddDate(0, 0, 1)}
}

func (p Period) HistoryCycles() []time.Time {
	quota := cycleQuota()
	cycles := make([]time.Time, 0, HistoryCycles)
	start := p.CycleStart
	for range HistoryCycles {
		cycles = append(cycles, start)
		start = quota.CycleStart(start.Add(-time.Nanosecond))
	}
	return cycles
}

func (p Period) HistorySince() time.Time {
	cycles := p.HistoryCycles()
	return cycles[len(cycles)-1]
}

type MonthUsage struct {
	CycleStart time.Time
	Requests   int64
}

func MonthsOf(p Period, rows []MonthUsage) []MonthUsage {
	cycles := p.HistoryCycles()
	months := make([]MonthUsage, len(cycles))
	for i, c := range cycles {
		months[i] = MonthUsage{CycleStart: c}
	}
	for _, row := range rows {
		i := slices.IndexFunc(cycles, func(c time.Time) bool { return c.Equal(row.CycleStart) })
		if i >= 0 {
			months[i].Requests = max(months[i].Requests, row.Requests)
		}
	}
	return months
}

type PlatformQuery struct {
	Page     int
	PageSize int
	Search   string
}

func (q PlatformQuery) Normalize() PlatformQuery {
	q.Page = min(max(q.Page, 1), PlatformMaxPage)
	switch {
	case q.PageSize <= 0:
		q.PageSize = PlatformDefaultPageSize
	case q.PageSize > PlatformMaxPageSize:
		q.PageSize = PlatformMaxPageSize
	}
	q.Search = strings.TrimSpace(q.Search)
	if runes := []rune(q.Search); len(runes) > PlatformSearchMax {
		q.Search = string(runes[:PlatformSearchMax])
	}
	return q
}

func (q PlatformQuery) Offset() int { return (q.Page - 1) * q.PageSize }

type PlatformWorkspace struct {
	ID   string
	Name string
}

type Coverage struct {
	leadmap.Summary
}

func (c Coverage) AddressShare() float64 {
	if c.Total <= 0 {
		return 0
	}
	return float64(c.Total-c.WithoutAddress) / float64(c.Total)
}

func (c Coverage) MapShare() float64 {
	if c.Total <= 0 {
		return 0
	}
	return float64(c.OnMap) / float64(c.Total)
}

type WorkspaceGeocoding struct {
	Workspace PlatformWorkspace
	Settings  Settings
	Usage     Usage
	Months    []MonthUsage
	Coverage  Coverage
}

type PlatformPage struct {
	Period     Period
	Items      []WorkspaceGeocoding
	Page       int
	PageSize   int
	TotalItems int64
}

func (p PlatformPage) TotalPages() int {
	if p.PageSize <= 0 || p.TotalItems <= 0 {
		return 0
	}
	return int((p.TotalItems + int64(p.PageSize) - 1) / int64(p.PageSize))
}

type PlatformDirectory interface {
	GeocodingWorkspaces(ctx context.Context, query PlatformQuery, cycleStart time.Time) ([]PlatformWorkspace, int64, error)
}

type UsageHistory interface {
	UsageOf(ctx context.Context, workspaceIDs []string) (map[string]Usage, error)
	History(ctx context.Context, workspaceIDs []string, since time.Time) (map[string][]MonthUsage, error)
}

type CoverageReader interface {
	Coverage(ctx context.Context, workspaceIDs []string) (map[string]leadmap.Summary, error)
}
