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
	platformCoverageCachePrefix = "geocoding:coverage"
	DefaultPlatformCoverageTTL  = 60 * time.Second
)

var errPlatformCoverageIncomplete = errors.New("geocoding platform coverage: a required dependency is missing")

type PlatformCoverageDeps struct {
	Directory geocoding.PlatformDirectory
	Coverage  geocoding.CoverageReader
	Memo      cache.Memo
	Gate      cache.Gate
	TTL       time.Duration
}

type PlatformCoverage struct {
	deps PlatformCoverageDeps
}

func NewPlatformCoverage(deps PlatformCoverageDeps) (*PlatformCoverage, error) {
	missing := map[string]bool{
		"directory": deps.Directory == nil,
		"coverage":  deps.Coverage == nil,
		"memo":      deps.Memo == nil,
		"gate":      deps.Gate == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errPlatformCoverageIncomplete, name)
		}
	}
	if deps.TTL <= 0 {
		deps.TTL = DefaultPlatformCoverageTTL
	}
	return &PlatformCoverage{deps: deps}, nil
}

func (u *PlatformCoverage) List(ctx context.Context, caller geocoding.Caller, query geocoding.PlatformQuery) (geocoding.PlatformPage, error) {
	if !caller.CanReadEveryWorkspace() {
		return geocoding.PlatformPage{}, geocoding.ErrPlatformForbidden
	}
	query = query.Normalize()
	return cache.Remember(ctx, u.deps.Memo, platformCoverageKey(query), u.deps.TTL, func(ctx context.Context) (geocoding.PlatformPage, error) {
		var page geocoding.PlatformPage
		err := cache.Gated(ctx, u.deps.Gate, func(ctx context.Context) error {
			var err error
			page, err = u.read(ctx, query)
			return err
		})
		return page, err
	})
}

func platformCoverageKey(query geocoding.PlatformQuery) string {
	search := sha256.Sum256([]byte(query.Search))
	return platformCoverageCachePrefix + ":" + strconv.Itoa(query.Page) + ":" + strconv.Itoa(query.PageSize) + ":" + hex.EncodeToString(search[:8])
}

func (u *PlatformCoverage) read(ctx context.Context, query geocoding.PlatformQuery) (geocoding.PlatformPage, error) {
	workspaces, total, err := u.deps.Directory.GeocodingWorkspaces(ctx, query)
	if err != nil {
		return geocoding.PlatformPage{}, fmt.Errorf("geocoding platform coverage: workspaces: %w", err)
	}
	page := geocoding.PlatformPage{Page: query.Page, PageSize: query.PageSize, TotalItems: total, Items: []geocoding.WorkspaceCoverage{}}
	if len(workspaces) == 0 {
		return page, nil
	}
	ids := make([]string, len(workspaces))
	for i, w := range workspaces {
		ids[i] = w.ID
	}
	coverage, err := u.deps.Coverage.Coverage(ctx, ids)
	if err != nil {
		return geocoding.PlatformPage{}, fmt.Errorf("geocoding platform coverage: coverage: %w", err)
	}
	for _, w := range workspaces {
		page.Items = append(page.Items, geocoding.WorkspaceCoverage{Workspace: w, Coverage: geocoding.Coverage{Summary: coverage[w.ID]}})
	}
	return page, nil
}
