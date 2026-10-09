package lead

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/shared"
)

const (
	DefaultRelativesPage = 50
	MaxRelativesPage     = 100
)

var ErrRelativesQueryInvalid = errors.New("lead: the relatives page asks for an unknown dimension, a negative size or a cursor this server did not give")

type RelativesQuery struct {
	LeadID    string
	Dimension RelationDimension
	After     string
	Limit     int
}

type RelativesCursor struct {
	CreatedAt  time.Time
	RelationID string
}

type Relative struct {
	Relation Relation
	Lead     *Lead
}

type RelativesPage struct {
	Relatives []Relative
	Next      string
}

type RelationTally struct {
	Version int64
	Counts  RelationCounts
}

type RelationWrite struct {
	Relation Relation
	Leads    map[string]RelationTally
}

type RelationStore interface {
	AddRelation(ctx context.Context, workspaceID string, r Relation) (RelationWrite, error)
	RemoveRelation(ctx context.Context, workspaceID, relationID, actorID string) (RelationWrite, error)
	ListRelatives(ctx context.Context, workspaceID string, q RelativesQuery) (RelativesPage, error)
}

func (r Relative) KindFrom(leadID string) RelationKind {
	return r.Relation.KindFor(leadID)
}

func (q RelativesQuery) Normalize() (RelativesQuery, error) {
	q.LeadID = strings.TrimSpace(q.LeadID)
	q.After = strings.TrimSpace(q.After)
	if q.LeadID == "" {
		return RelativesQuery{}, ErrLeadRequired
	}
	if q.Dimension != "" && q.Dimension != DimensionFamily && q.Dimension != DimensionReferral {
		return RelativesQuery{}, ErrRelativesQueryInvalid
	}
	switch {
	case q.Limit < 0:
		return RelativesQuery{}, ErrRelativesQueryInvalid
	case q.Limit == 0:
		q.Limit = DefaultRelativesPage
	case q.Limit > MaxRelativesPage:
		q.Limit = MaxRelativesPage
	}
	if _, _, err := q.Cursor(); err != nil {
		return RelativesQuery{}, err
	}
	return q, nil
}

func (q RelativesQuery) Cursor() (RelativesCursor, bool, error) {
	key, paged, err := shared.ParseKeyset(q.After)
	if err != nil {
		return RelativesCursor{}, false, ErrRelativesQueryInvalid
	}
	if !paged {
		return RelativesCursor{}, false, nil
	}
	parsed, err := uuid.Parse(key.ID)
	if err != nil {
		return RelativesCursor{}, false, ErrRelativesQueryInvalid
	}
	return RelativesCursor{CreatedAt: key.At, RelationID: parsed.String()}, true, nil
}

func (c RelativesCursor) Encode() string {
	return shared.Keyset{At: c.CreatedAt, ID: c.RelationID}.Encode()
}
