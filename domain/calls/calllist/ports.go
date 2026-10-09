package calllist

import (
	"context"
	"time"

	"vozko/domain/calls/cdr"
	"vozko/domain/lead"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
	DefaultItemPage = 50
	MaxItemPage     = 200
)

type ListQuery struct {
	WorkspaceID string
	ViewerID    string
	Manages     bool
	Status      Status
	Page        int
	PageSize    int
}

func (q ListQuery) Normalized() ListQuery {
	if q.Page < 1 {
		q.Page = 1
	}
	switch {
	case q.PageSize <= 0:
		q.PageSize = DefaultPageSize
	case q.PageSize > MaxPageSize:
		q.PageSize = MaxPageSize
	}
	return q
}

type ListPage struct {
	Lists []*List
	Total int64
}

type ItemQuery struct {
	WorkspaceID   string
	ListID        string
	State         State
	AfterPosition int
	AfterAt       *time.Time
	AsOf          time.Time
	Limit         int
}

func (q ItemQuery) Normalized() ItemQuery {
	if q.AfterPosition < 0 {
		q.AfterPosition = 0
	}
	q.AsOf = q.AsOf.UTC()
	if q.AfterAt != nil {
		afterAt := q.AfterAt.UTC()
		q.AfterAt = &afterAt
	}
	switch {
	case q.Limit <= 0:
		q.Limit = DefaultItemPage
	case q.Limit > MaxItemPage:
		q.Limit = MaxItemPage
	}
	return q
}

type ItemView struct {
	Item
	LeadName     string
	LeadNumber   string
	LeadDistrict string
	LeadCity     string
	LastCall     *cdr.Call
	StampedCall  *CallFacts
	Attempts     int
}

func (v ItemView) LeadRealName() string {
	return (&lead.Lead{Name: v.LeadName, Number: v.LeadNumber}).RealName()
}

type ItemPage struct {
	Items  []ItemView
	Next   int
	NextAt *time.Time
	AsOf   *time.Time
}

type NextClaim struct {
	WorkspaceID string
	ListID      string
	UserID      string
	Now         time.Time
}

type BuildRef struct {
	WorkspaceID string
	ID          string
}

type BuildBatch struct {
	WorkspaceID string
	ListID      string
	Claim       string
	Items       []Item
	Skipped     Skips
	Cursor      string
	At          time.Time
}

type Mutation func(item *Item, list *List) error

type Store interface {
	Create(ctx context.Context, l *List) (*List, bool, error)
	Get(ctx context.Context, workspaceID, id string) (*List, error)
	Page(ctx context.Context, q ListQuery) (ListPage, error)
	Update(ctx context.Context, l *List) error
	Delete(ctx context.Context, workspaceID, id string) error

	ClaimBuild(ctx context.Context, workspaceID, id, token string, now time.Time) (*List, error)
	AppendItems(ctx context.Context, b BuildBatch) error
	FinishBuild(ctx context.Context, workspaceID, id, token string, status Status, failureCode string, now time.Time) error
	Buildable(ctx context.Context, now time.Time, limit int) ([]BuildRef, error)
	FailExhausted(ctx context.Context, now time.Time) (int64, error)

	Items(ctx context.Context, q ItemQuery) (ItemPage, error)
	Item(ctx context.Context, workspaceID, id string) (*Item, error)
	Next(ctx context.Context, c NextClaim) (*Item, error)
	Mutate(ctx context.Context, workspaceID, itemID string, fn Mutation) (*Item, error)
}
