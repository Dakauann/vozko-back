package lead

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/crmfilter"
	"vozko/domain/selection"
	"vozko/domain/shared"
)

const SelectionRefType = "lead"

var ErrAssignmentInvalid = errors.New("lead selection: the pending assignment names no known field and value")

type AssignmentKind string

const (
	AssignCustomField AssignmentKind = "custom_field"
	AssignOwner       AssignmentKind = "owner"
	AssignBlocked     AssignmentKind = "blocked"
)

type Assignment struct {
	Kind  AssignmentKind
	Key   string
	Value any
}

func (a Assignment) Validate() error {
	switch a.Kind {
	case AssignCustomField:
		if strings.TrimSpace(a.Key) == "" {
			return ErrAssignmentInvalid
		}
	case AssignOwner:
		if _, ok := a.Value.(string); !ok {
			return ErrAssignmentInvalid
		}
	case AssignBlocked:
		if _, ok := a.Value.(bool); !ok {
			return ErrAssignmentInvalid
		}
	default:
		return fmt.Errorf("%w: %q", ErrAssignmentInvalid, a.Kind)
	}
	return nil
}

type SelectionQuery struct {
	WorkspaceID string
	Filter      crmfilter.Filter
	Today       time.Time
	IDs         []string
	ExcludeIDs  []string
	Require     crmfilter.Filter
	Pending     *Assignment
	Order       []shared.Sort
	Limit       int
}

func (q SelectionQuery) Ordered() bool {
	return q.Limit > 0
}

func (q SelectionQuery) Validate() error {
	if strings.TrimSpace(q.WorkspaceID) == "" {
		return ErrLeadWorkspaceRequired
	}
	if q.Limit < 0 || (q.Limit > 0 && len(q.IDs) > 0) {
		return selection.ErrLimitRequired
	}
	if q.Pending != nil {
		return q.Pending.Validate()
	}
	return nil
}

type Frozen struct {
	Size    int
	Existed bool
}

type SelectionReader interface {
	CountSelection(ctx context.Context, q SelectionQuery) (int, error)
	SelectionPage(ctx context.Context, q SelectionQuery, after string, limit int) ([]string, error)
}

type SelectionSnapshots interface {
	FreezeSelection(ctx context.Context, q SelectionQuery, snapshotID string) (Frozen, error)
	SnapshotPage(ctx context.Context, workspaceID, snapshotID, after string, limit int) ([]string, error)
	SnapshotSize(ctx context.Context, workspaceID, snapshotID string) (int, error)
	DropSnapshot(ctx context.Context, workspaceID, snapshotID string) error
	DropSnapshotsBefore(ctx context.Context, before time.Time, limit int) (int64, error)
}

func SelectionOrder(sorts []crmfilter.Sort) ([]shared.Sort, error) {
	if len(sorts) == 0 {
		return []shared.Sort{DefaultSort}, nil
	}
	order := make([]shared.Sort, 0, len(sorts))
	for _, s := range sorts {
		key, ok := ParseSortKey(string(s.Field))
		if !ok {
			return nil, fmt.Errorf("%w: %q", selection.ErrUnknownSort, s.Field)
		}
		direction := shared.SortAsc
		if s.Desc {
			direction = shared.SortDesc
		}
		order = append(order, shared.Sort{Field: string(key), Direction: direction})
	}
	return order, nil
}

func SelectionIDs(ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, raw := range ids {
		parsed, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("%w: %q", selection.ErrInvalidIDs, raw)
		}
		id := parsed.String()
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}
