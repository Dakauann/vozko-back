package leadarea

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

const (
	workspaceID = "0b6f9c1e-7d1a-4c61-9a0e-2f8d4c1b2a10"
	ownerID     = "7d3e1f00-1111-4c2b-8f00-aa00bb00cc01"
	otherID     = "7d3e1f00-2222-4c2b-8f00-aa00bb00cc02"
)

var now = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func rectangle() geo.Shape {
	return geo.Shape{Kind: geo.ShapeRectangle, Ring: []geo.Point{
		{Lat: -23.57, Lng: -46.67}, {Lat: -23.57, Lng: -46.64}, {Lat: -23.55, Lng: -46.64}, {Lat: -23.55, Lng: -46.67},
	}}
}

func circle() geo.Shape {
	return geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -19.92, Lng: -43.94}, RadiusM: 1500}
}

func TestNewAreaKeepsTheOwnerAndTheShape(t *testing.T) {
	a, err := New(workspaceID, ownerID, Draft{Name: "  Território Norte  ", Visibility: shared.VisibilityShared, Shape: rectangle()}, now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Território Norte" || a.OwnerID != ownerID || a.WorkspaceID != workspaceID {
		t.Fatalf("area = %+v", a)
	}
	if a.CreatedAt != now || a.UpdatedAt != now {
		t.Fatalf("timestamps = %v %v", a.CreatedAt, a.UpdatedAt)
	}
	if len(a.Ring()) != 4 {
		t.Fatalf("a rectangle is stored as its four corners, got %v", a.Ring())
	}
}

func TestNewAreaStoresACircleAsA64VertexRing(t *testing.T) {
	a, err := New(workspaceID, ownerID, Draft{Name: "Raio da escola", Shape: circle()}, now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Visibility != shared.VisibilityPrivate {
		t.Fatalf("an area is private unless shared, got %q", a.Visibility)
	}
	if len(a.Ring()) != geo.CircleVertices {
		t.Fatalf("ring has %d vertices, want %d", len(a.Ring()), geo.CircleVertices)
	}
}

func TestNewAreaRefusals(t *testing.T) {
	tests := []struct {
		name  string
		ws    string
		owner string
		draft Draft
		want  error
	}{
		{"no workspace", "", ownerID, Draft{Name: "A", Shape: rectangle()}, ErrWorkspaceRequired},
		{"no owner", workspaceID, " ", Draft{Name: "A", Shape: rectangle()}, ErrOwnerRequired},
		{"a blank name", workspaceID, ownerID, Draft{Name: "   ", Shape: rectangle()}, ErrNameRequired},
		{"a name past the limit", workspaceID, ownerID, Draft{Name: strings.Repeat("a", MaxNameLength+1), Shape: rectangle()}, ErrNameTooLong},
		{"an unknown visibility", workspaceID, ownerID, Draft{Name: "A", Visibility: "public", Shape: rectangle()}, ErrVisibilityInvalid},
		{"no shape", workspaceID, ownerID, Draft{Name: "A"}, ErrShapeInvalid},
		{"a radius past 50 km", workspaceID, ownerID, Draft{Name: "A", Shape: geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -19.9, Lng: -43.9}, RadiusM: 50001}}, ErrShapeInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.ws, tt.owner, tt.draft, now); !errors.Is(err, tt.want) {
				t.Fatalf("New() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestShapeRefusalsKeepTheGeoReason(t *testing.T) {
	_, err := New(workspaceID, ownerID, Draft{Name: "A", Shape: geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -19.9, Lng: -43.9}, RadiusM: 50001}}, now)
	if !errors.Is(err, geo.ErrInvalidRadius) {
		t.Fatalf("err = %v, want the geo reason kept", err)
	}
}

func TestOnlyTheOwnerChangesAnArea(t *testing.T) {
	a, err := New(workspaceID, ownerID, Draft{Name: "Centro", Visibility: shared.VisibilityShared, Shape: rectangle()}, now)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	name := "Centro expandido"
	if _, err := a.Apply(Patch{Name: &name}, otherID, later); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a shared area is readable, never editable, by others: %v", err)
	}
	private := shared.VisibilityPrivate
	shape := circle()
	changed, err := a.Apply(Patch{Name: &name, Visibility: &private, Shape: &shape}, ownerID, later)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Name != name || changed.Visibility != private || changed.Shape.Kind != geo.ShapeCircle {
		t.Fatalf("changed = %+v", changed)
	}
	if changed.UpdatedAt != later || changed.CreatedAt != now || changed.OwnerID != ownerID {
		t.Fatalf("changed timestamps or owner = %+v", changed)
	}
	if a.Name != "Centro" {
		t.Fatal("Apply must not touch the original")
	}
	blank := " "
	if _, err := a.Apply(Patch{Name: &blank}, ownerID, later); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("a patch is validated like a new area: %v", err)
	}
}

func TestReadAndEditFollowTheSharedOwnershipRule(t *testing.T) {
	private, _ := New(workspaceID, ownerID, Draft{Name: "Minha", Shape: rectangle()}, now)
	shared_, _ := New(workspaceID, ownerID, Draft{Name: "Nossa", Visibility: shared.VisibilityShared, Shape: rectangle()}, now)
	if !private.Owned().CanRead(ownerID) || private.Owned().CanRead(otherID) {
		t.Fatal("a private area is read by its owner only")
	}
	if !shared_.Owned().CanRead(otherID) || shared_.Owned().CanEdit(otherID) {
		t.Fatal("a shared area is read by everyone and edited by its owner only")
	}
}

func TestErrorCodes(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{ErrNotFound, "area_not_found"},
		{ErrForbidden, "area_forbidden"},
		{ErrNameRequired, "area_name_required"},
		{ErrNameTooLong, "area_name_too_long"},
		{ErrVisibilityInvalid, "area_visibility_invalid"},
		{ErrShapeInvalid, "area_shape_invalid"},
		{ErrTooManyAreas, "area_too_many"},
		{ErrOperatorUnsupported, "area_operator_unsupported"},
	}
	for _, tt := range tests {
		if got := ErrorCode(tt.err); got != tt.code {
			t.Fatalf("ErrorCode(%v) = %q, want %q", tt.err, got, tt.code)
		}
	}
	if !IsInputRefusal(ErrShapeInvalid) || IsInputRefusal(ErrNotFound) || IsInputRefusal(ErrForbidden) {
		t.Fatal("input refusals are the 400s, not the 404 or 403")
	}
}

func TestAreasNeedTheSameAddressPermissionAsLeads(t *testing.T) {
	if !errors.Is(ErrAddressesRequired, lead.ErrAddressesForbidden) || ErrorCode(ErrAddressesRequired) != lead.ErrorCode(lead.ErrAddressesForbidden) {
		t.Fatal("an area refused for addresses is the lead address refusal, with its code")
	}
	actions := RequiredActions()
	if len(actions) != 2 || actions[0] != workspace.ActionRead || actions[1] != workspace.ActionReadAddresses {
		t.Fatalf("required actions = %v, want leads read and read addresses", actions)
	}
}

func TestAWorkspaceHoldsAtMostTheListedNumberOfAreas(t *testing.T) {
	if err := CheckRoomFor(MaxPerWorkspace - 1); err != nil {
		t.Fatalf("one below the cap = %v, want room", err)
	}
	if err := CheckRoomFor(MaxPerWorkspace); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("at the cap = %v, want ErrLimitReached", err)
	}
	if MaxPerWorkspace > MaxListed {
		t.Fatal("every area a workspace can hold must fit in the list")
	}
	if ErrorCode(ErrLimitReached) != "area_limit_reached" || !IsInputRefusal(ErrLimitReached) {
		t.Fatal("the cap is a coded input refusal")
	}
}

func TestBoundsHoldTheWholeRing(t *testing.T) {
	a, err := New(workspaceID, ownerID, Draft{Name: "Centro", Shape: circle()}, now)
	if err != nil {
		t.Fatal(err)
	}
	b := a.Bounds()
	if !b.UpdatedAt.Equal(a.UpdatedAt) {
		t.Fatalf("bounds edit time = %v, want the area's %v", b.UpdatedAt, a.UpdatedAt)
	}
	if b.ID != "" ||b.South >= -19.92 || b.North <= -19.92 || b.West >= -43.94 || b.East <= -43.94 {
		t.Fatalf("bounds = %+v, want the circle around its center", b)
	}
	for _, p := range a.Ring() {
		if p.Lat < b.South || p.Lat > b.North || p.Lng < b.West || p.Lng > b.East {
			t.Fatalf("ring point %+v is outside the bounds %+v", p, b)
		}
	}
}
