package leadarea

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

const (
	MaxNameLength   = 80
	MaxListed       = 500
	MaxPerWorkspace = MaxListed
)

var (
	ErrWorkspaceRequired   = errors.New("leadarea: workspace is required")
	ErrOwnerRequired       = errors.New("leadarea: owner is required")
	ErrNotFound            = errors.New("leadarea: area not found")
	ErrForbidden           = errors.New("leadarea: only the owner changes an area")
	ErrAddressesRequired   = lead.ErrAddressesForbidden
	ErrNameRequired        = errors.New("leadarea: name is required")
	ErrNameTooLong         = errors.New("leadarea: name is too long")
	ErrVisibilityInvalid   = errors.New("leadarea: visibility must be private or shared")
	ErrShapeInvalid        = errors.New("leadarea: invalid shape")
	ErrTooManyAreas        = errors.New("leadarea: too many areas in one predicate")
	ErrOperatorUnsupported = errors.New("leadarea: an area predicate only accepts the in operator")
	ErrLimitReached        = errors.New("leadarea: the workspace holds as many areas as it can")
	ErrLeftOutUnsupported  = errors.New("leadarea: the leads left out of an area are counted only when every group holding an area either joins its tests with and or tests nothing but areas")
)

type Area struct {
	ID          string
	WorkspaceID string
	OwnerID     string
	Visibility  shared.Visibility
	Name        string
	Shape       geo.Shape
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Draft struct {
	Name       string
	Visibility shared.Visibility
	Shape      geo.Shape
}

type Patch struct {
	Name       *string
	Visibility *shared.Visibility
	Shape      *geo.Shape
}

type Repository interface {
	Create(ctx context.Context, a Area) error
	Update(ctx context.Context, a Area) error
	Delete(ctx context.Context, workspaceID, id string, at time.Time) error
	Get(ctx context.Context, workspaceID, id string) (Area, error)
	ListReadable(ctx context.Context, workspaceID, viewerID string) ([]Area, error)
	FindLive(ctx context.Context, workspaceID string, ids []string) ([]Area, error)
	CountLive(ctx context.Context, workspaceID string) (int, error)
}

func New(workspaceID, ownerID string, d Draft, now time.Time) (Area, error) {
	a := Area{
		WorkspaceID: strings.TrimSpace(workspaceID),
		OwnerID:     strings.TrimSpace(ownerID),
		Visibility:  d.Visibility,
		Name:        d.Name,
		Shape:       d.Shape,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if a.Visibility == "" {
		a.Visibility = shared.VisibilityPrivate
	}
	return a.normalized()
}

func (a Area) Apply(p Patch, editorID string, now time.Time) (Area, error) {
	if !a.Owned().CanEdit(editorID) {
		return Area{}, ErrForbidden
	}
	next := a
	if p.Name != nil {
		next.Name = *p.Name
	}
	if p.Visibility != nil {
		next.Visibility = *p.Visibility
	}
	if p.Shape != nil {
		next.Shape = *p.Shape
	}
	next.UpdatedAt = now
	return next.normalized()
}

func (a Area) Owned() shared.Owned {
	return shared.Owned{OwnerID: a.OwnerID, Visibility: a.Visibility}
}

func (a Area) Ring() []geo.Point {
	return a.Shape.Ring64()
}

func (a Area) Bounds() crmfilter.AreaBounds {
	ring := a.Ring()
	if len(ring) == 0 {
		return crmfilter.AreaBounds{ID: a.ID, UpdatedAt: a.UpdatedAt}
	}
	b := crmfilter.AreaBounds{ID: a.ID, South: ring[0].Lat, West: ring[0].Lng, North: ring[0].Lat, East: ring[0].Lng, UpdatedAt: a.UpdatedAt}
	for _, p := range ring[1:] {
		b.South, b.North = min(b.South, p.Lat), max(b.North, p.Lat)
		b.West, b.East = min(b.West, p.Lng), max(b.East, p.Lng)
	}
	return b
}

func RequiredActions() []workspace.Action {
	return []workspace.Action{workspace.ActionRead, workspace.ActionReadAddresses}
}

func CheckRoomFor(live int) error {
	if live >= MaxPerWorkspace {
		return fmt.Errorf("%w: %d of %d", ErrLimitReached, live, MaxPerWorkspace)
	}
	return nil
}

func (a Area) normalized() (Area, error) {
	a.Name = strings.TrimSpace(a.Name)
	switch {
	case a.WorkspaceID == "":
		return Area{}, ErrWorkspaceRequired
	case a.OwnerID == "":
		return Area{}, ErrOwnerRequired
	case a.Name == "":
		return Area{}, ErrNameRequired
	case utf8.RuneCountInString(a.Name) > MaxNameLength:
		return Area{}, ErrNameTooLong
	case !a.Visibility.Valid():
		return Area{}, ErrVisibilityInvalid
	}
	if err := a.Shape.Validate(); err != nil {
		return Area{}, fmt.Errorf("%w: %w", ErrShapeInvalid, err)
	}
	return a, nil
}

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrNotFound, "area_not_found"},
	{ErrForbidden, "area_forbidden"},
	{ErrAddressesRequired, "lead_addresses_forbidden"},
	{ErrNameRequired, "area_name_required"},
	{ErrNameTooLong, "area_name_too_long"},
	{ErrVisibilityInvalid, "area_visibility_invalid"},
	{ErrShapeInvalid, "area_shape_invalid"},
	{ErrTooManyAreas, "area_too_many"},
	{ErrOperatorUnsupported, "area_operator_unsupported"},
	{ErrLimitReached, "area_limit_reached"},
	{ErrLeftOutUnsupported, "area_left_out_unsupported"},
	{ErrWorkspaceRequired, "workspace_required"},
	{ErrOwnerRequired, "area_owner_required"},
}

var inputRefusals = []error{ErrNameRequired, ErrNameTooLong, ErrVisibilityInvalid, ErrShapeInvalid, ErrTooManyAreas, ErrOperatorUnsupported, ErrLimitReached, ErrLeftOutUnsupported}

func ErrorCode(err error) string {
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}

func IsInputRefusal(err error) bool {
	for _, refusal := range inputRefusals {
		if errors.Is(err, refusal) {
			return true
		}
	}
	return false
}
