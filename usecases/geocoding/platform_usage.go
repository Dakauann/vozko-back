package geocoding_usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"vozko/domain/cache"
	"vozko/domain/geocoding"
)

const (
	platformUsageCachePrefix = "geocoding:platform"
	DefaultPlatformUsageTTL  = 60 * time.Second
)

var errPlatformUsageIncomplete = errors.New("geocoding platform usage: a required dependency is missing")

type PlatformUsageDeps struct {
	Directory geocoding.PlatformDirectory
	Settings  geocoding.SettingsStore
	Usage     geocoding.UsageHistory
	Coverage  geocoding.CoverageReader
	Memo      cache.Memo
	Gate      cache.Gate
	Now       func() time.Time
	TTL       time.Duration
}

type PlatformUsage struct {
	deps PlatformUsageDeps
}

func NewPlatformUsage(deps PlatformUsageDeps) (*PlatformUsage, error) {
	missing := map[string]bool{
		"directory": deps.Directory == nil,
		"settings":  deps.Settings == nil,
		"usage":     deps.Usage == nil,
		"coverage":  deps.Coverage == nil,
		"memo":      deps.Memo == nil,
		"gate":      deps.Gate == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errPlatformUsageIncomplete, name)
		}
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.TTL <= 0 {
		deps.TTL = DefaultPlatformUsageTTL
	}
	return &PlatformUsage{deps: deps}, nil
}

func (u *PlatformUsage) List(ctx context.Context, editor geocoding.Editor, query geocoding.PlatformQuery) (geocoding.PlatformPage, error) {
	if !editor.CanReadPlatformUsage() {
		return geocoding.PlatformPage{}, geocoding.ErrPlatformForbidden
	}
	query = query.Normalize()
	period := geocoding.PeriodAt(u.deps.Now())
	return cache.Remember(ctx, u.deps.Memo, platformUsageKey(query, period), u.deps.TTL, func(ctx context.Context) (geocoding.PlatformPage, error) {
		var page geocoding.PlatformPage
		err := cache.Gated(ctx, u.deps.Gate, func(ctx context.Context) error {
			var err error
			page, err = u.read(ctx, query, period)
			return err
		})
		return page, err
	})
}

func platformUsageKey(query geocoding.PlatformQuery, period geocoding.Period) string {
	search := sha256.Sum256([]byte(query.Search))
	return platformUsageCachePrefix + ":" + strconv.FormatInt(period.CycleStart.Unix(), 10) + ":" + strconv.FormatInt(period.Day.Unix(), 10) +
		":" + strconv.Itoa(query.Page) + ":" + strconv.Itoa(query.PageSize) + ":" + hex.EncodeToString(search[:8])
}

func (u *PlatformUsage) read(ctx context.Context, query geocoding.PlatformQuery, period geocoding.Period) (geocoding.PlatformPage, error) {
	workspaces, total, err := u.deps.Directory.GeocodingWorkspaces(ctx, query, period.CycleStart)
	if err != nil {
		return geocoding.PlatformPage{}, fmt.Errorf("geocoding platform usage: workspaces: %w", err)
	}
	page := geocoding.PlatformPage{Period: period, Page: query.Page, PageSize: query.PageSize, TotalItems: total, Items: []geocoding.WorkspaceGeocoding{}}
	if len(workspaces) == 0 {
		return page, nil
	}
	ids := make([]string, len(workspaces))
	for i, w := range workspaces {
		ids[i] = w.ID
	}
	settings, err := u.deps.Settings.SettingsOf(ctx, ids)
	if err != nil {
		return geocoding.PlatformPage{}, fmt.Errorf("geocoding platform usage: settings: %w", err)
	}
	usage, err := u.deps.Usage.UsageOf(ctx, ids)
	if err != nil {
		return geocoding.PlatformPage{}, fmt.Errorf("geocoding platform usage: usage: %w", err)
	}
	history, err := u.deps.Usage.History(ctx, ids, period.HistorySince())
	if err != nil {
		return geocoding.PlatformPage{}, fmt.Errorf("geocoding platform usage: history: %w", err)
	}
	coverage, err := u.deps.Coverage.Coverage(ctx, ids)
	if err != nil {
		return geocoding.PlatformPage{}, fmt.Errorf("geocoding platform usage: coverage: %w", err)
	}
	for _, w := range workspaces {
		s, ok := settings[w.ID]
		if !ok {
			s = geocoding.Settings{WorkspaceID: w.ID}
		}
		page.Items = append(page.Items, geocoding.WorkspaceGeocoding{
			Workspace: w,
			Settings:  s,
			Usage:     usage[w.ID].At(period),
			Months:    geocoding.MonthsOf(period, history[w.ID]),
			Coverage:  geocoding.Coverage{Summary: coverage[w.ID]},
		})
	}
	return page, nil
}
