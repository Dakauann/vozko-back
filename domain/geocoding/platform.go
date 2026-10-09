package geocoding

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/leadmap"
)

const (
	PlatformDefaultPageSize = 20
	PlatformMaxPageSize     = 50
	PlatformSearchMax       = 100
	PlatformMaxPage         = 1_000_000
)

var ErrPlatformForbidden = errors.New("geocoding: only a platform admin can read the address coverage of every workspace")

type Caller struct {
	UserID        string
	PlatformAdmin bool
}

func (c Caller) CanReadEveryWorkspace() bool {
	return strings.TrimSpace(c.UserID) != "" && c.PlatformAdmin
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

type WorkspaceCoverage struct {
	Workspace PlatformWorkspace
	Coverage  Coverage
}

type PlatformPage struct {
	Items      []WorkspaceCoverage
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
	GeocodingWorkspaces(ctx context.Context, query PlatformQuery) ([]PlatformWorkspace, int64, error)
}

type CoverageReader interface {
	Coverage(ctx context.Context, workspaceIDs []string) (map[string]leadmap.Summary, error)
}
