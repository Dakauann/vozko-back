package leadarea_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
	"vozko/domain/geo"
	"vozko/domain/leadarea"
	"vozko/domain/shared"
)

const (
	workspaceID = "0b6f9c1e-7d1a-4c61-9a0e-2f8d4c1b2a10"
	ownerID     = "7d3e1f00-1111-4c2b-8f00-aa00bb00cc01"
	otherID     = "7d3e1f00-2222-4c2b-8f00-aa00bb00cc02"
	areaID      = "11111111-1111-4111-8111-111111111111"
)

var now = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

type permissions map[string]bool

func (p permissions) HasWorkspacePermission(userID, ws, resource, action string, _ bool) bool {
	return userID != "" && ws == workspaceID && p[resource+":"+action]
}

var mapper = permissions{"leads:read": true, "leads:read_addresses": true}

type memoryAreas struct {
	byID      map[string]leadarea.Area
	deleted   map[string]bool
	findErr   error
	findSeen  [][]string
	countErr  error
	extraLive int
}

func newMemoryAreas(areas ...leadarea.Area) *memoryAreas {
	m := &memoryAreas{byID: map[string]leadarea.Area{}, deleted: map[string]bool{}}
	for _, a := range areas {
		m.byID[a.ID] = a
	}
	return m
}

func (m *memoryAreas) Create(_ context.Context, a leadarea.Area) error {
	m.byID[a.ID] = a
	return nil
}

func (m *memoryAreas) Update(_ context.Context, a leadarea.Area) error {
	if _, ok := m.byID[a.ID]; !ok || m.deleted[a.ID] {
		return leadarea.ErrNotFound
	}
	m.byID[a.ID] = a
	return nil
}

func (m *memoryAreas) Delete(_ context.Context, ws, id string, _ time.Time) error {
	if a, ok := m.byID[id]; !ok || a.WorkspaceID != ws || m.deleted[id] {
		return leadarea.ErrNotFound
	}
	m.deleted[id] = true
	return nil
}

func (m *memoryAreas) Get(_ context.Context, ws, id string) (leadarea.Area, error) {
	a, ok := m.byID[id]
	if !ok || a.WorkspaceID != ws || m.deleted[id] {
		return leadarea.Area{}, leadarea.ErrNotFound
	}
	return a, nil
}

func (m *memoryAreas) ListReadable(_ context.Context, ws, viewer string) ([]leadarea.Area, error) {
	var out []leadarea.Area
	for _, a := range m.byID {
		if a.WorkspaceID == ws && !m.deleted[a.ID] {
			out = append(out, a)
		}
	}
	return out, nil
}

func (m *memoryAreas) FindLive(_ context.Context, ws string, ids []string) ([]leadarea.Area, error) {
	m.findSeen = append(m.findSeen, ids)
	if m.findErr != nil {
		return nil, m.findErr
	}
	var out []leadarea.Area
	for _, id := range ids {
		if a, ok := m.byID[id]; ok && a.WorkspaceID == ws && !m.deleted[id] {
			out = append(out, a)
		}
	}
	return out, nil
}

func (m *memoryAreas) CountLive(_ context.Context, ws string) (int, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	n := 0
	for _, a := range m.byID {
		if a.WorkspaceID == ws && !m.deleted[a.ID] {
			n++
		}
	}
	return n + m.extraLive, nil
}

func actor(user string) conversation.Viewer {
	return conversation.Viewer{UserID: user, WorkspaceID: workspaceID}
}

func rectangle() geo.Shape {
	return geo.Shape{Kind: geo.ShapeRectangle, Ring: []geo.Point{
		{Lat: -23.57, Lng: -46.67}, {Lat: -23.57, Lng: -46.64}, {Lat: -23.55, Lng: -46.64}, {Lat: -23.55, Lng: -46.67},
	}}
}

func storedArea(owner string, visibility shared.Visibility) leadarea.Area {
	return leadarea.Area{ID: areaID, WorkspaceID: workspaceID, OwnerID: owner, Visibility: visibility, Name: "Centro", Shape: rectangle(), CreatedAt: now, UpdatedAt: now}
}

func service(t *testing.T, perms permissions, areas *memoryAreas) *Service {
	t.Helper()
	s, err := New(Deps{Areas: areas, Permissions: perms, Now: func() time.Time { return now }, NewID: func() string { return areaID }})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewRefusesAMissingDependency(t *testing.T) {
	cases := map[string]Deps{
		"areas":       {Permissions: mapper},
		"permissions": {Areas: newMemoryAreas()},
	}
	for name, deps := range cases {
		if _, err := New(deps); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("missing %s = %v", name, err)
		}
	}
}

func TestCreateStoresTheAreaForItsOwner(t *testing.T) {
	areas := newMemoryAreas()
	a, err := service(t, mapper, areas).Create(context.Background(), actor(ownerID), leadarea.Draft{Name: "Território Norte", Shape: rectangle()})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != areaID || a.OwnerID != ownerID || a.WorkspaceID != workspaceID || a.Visibility != shared.VisibilityPrivate {
		t.Fatalf("area = %+v", a)
	}
	if _, stored := areas.byID[areaID]; !stored {
		t.Fatal("the area was not stored")
	}
}

func TestEveryAreaRouteNeedsTheFullAddressPermission(t *testing.T) {
	areas := newMemoryAreas(storedArea(ownerID, shared.VisibilityShared))
	noAddresses := service(t, permissions{"leads:read": true}, areas)
	ctx := context.Background()
	if _, err := noAddresses.Create(ctx, actor(ownerID), leadarea.Draft{Name: "A", Shape: rectangle()}); !errors.Is(err, leadarea.ErrAddressesRequired) {
		t.Fatalf("create = %v", err)
	}
	if _, err := noAddresses.List(ctx, actor(ownerID)); !errors.Is(err, leadarea.ErrAddressesRequired) {
		t.Fatalf("list = %v", err)
	}
	if _, err := noAddresses.Get(ctx, actor(ownerID), areaID); !errors.Is(err, leadarea.ErrAddressesRequired) {
		t.Fatalf("get = %v", err)
	}
	name := "B"
	if _, err := noAddresses.Update(ctx, actor(ownerID), areaID, leadarea.Patch{Name: &name}); !errors.Is(err, leadarea.ErrAddressesRequired) {
		t.Fatalf("update = %v", err)
	}
	if err := noAddresses.Delete(ctx, actor(ownerID), areaID); !errors.Is(err, leadarea.ErrAddressesRequired) {
		t.Fatalf("delete = %v", err)
	}
	addressesOnly := service(t, permissions{"leads:read_addresses": true}, areas)
	if _, err := addressesOnly.List(ctx, actor(ownerID)); !errors.Is(err, leadarea.ErrAddressesRequired) {
		t.Fatalf("the capability needs leads:read too: %v", err)
	}
}

func TestOthersReadASharedAreaButNeverChangeIt(t *testing.T) {
	areas := newMemoryAreas(storedArea(ownerID, shared.VisibilityShared))
	s := service(t, mapper, areas)
	ctx := context.Background()
	if _, err := s.Get(ctx, actor(otherID), areaID); err != nil {
		t.Fatalf("a shared area is readable: %v", err)
	}
	name := "Meu agora"
	if _, err := s.Update(ctx, actor(otherID), areaID, leadarea.Patch{Name: &name}); !errors.Is(err, leadarea.ErrForbidden) {
		t.Fatalf("update by another member = %v, want leadarea.ErrForbidden", err)
	}
	if err := s.Delete(ctx, actor(otherID), areaID); !errors.Is(err, leadarea.ErrForbidden) {
		t.Fatalf("delete by another member = %v", err)
	}
	updated, err := s.Update(ctx, actor(ownerID), areaID, leadarea.Patch{Name: &name})
	if err != nil || updated.Name != name {
		t.Fatalf("the owner renames: %+v %v", updated, err)
	}
	if err := s.Delete(ctx, actor(ownerID), areaID); err != nil {
		t.Fatal(err)
	}
}

func TestAnotherMembersPrivateAreaDoesNotExistForYou(t *testing.T) {
	s := service(t, mapper, newMemoryAreas(storedArea(otherID, shared.VisibilityPrivate)))
	ctx := context.Background()
	if _, err := s.Get(ctx, actor(ownerID), areaID); !errors.Is(err, leadarea.ErrNotFound) {
		t.Fatalf("get = %v, want ErrNotFound", err)
	}
	name := "x"
	if _, err := s.Update(ctx, actor(ownerID), areaID, leadarea.Patch{Name: &name}); !errors.Is(err, leadarea.ErrNotFound) {
		t.Fatalf("update = %v, want ErrNotFound", err)
	}
	if err := s.Delete(ctx, actor(ownerID), areaID); !errors.Is(err, leadarea.ErrNotFound) {
		t.Fatalf("delete = %v, want ErrNotFound", err)
	}
	listed, err := s.List(ctx, actor(ownerID))
	if err != nil || len(listed) != 0 {
		t.Fatalf("list = %+v %v, want the private area of another member left out", listed, err)
	}
}

func areaFilter(ids ...string) crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldArea, Operator: crmfilter.OpIn, Values: ids},
	}}}}
}

func TestBindAreasResolvesEveryAreaBeforeAnyFilterCompiles(t *testing.T) {
	areas := newMemoryAreas(storedArea(ownerID, shared.VisibilityShared))
	s := service(t, mapper, areas)
	ctx := context.Background()

	bound, stamps, err := s.BindAreas(ctx, actor(otherID), areaFilter(areaID))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bound.Groups[0].Predicates[0].BoundKind(); !ok || len(stamps) != 1 {
		t.Fatalf("bound = %+v stamps = %v", bound, stamps)
	}

	if err := areas.Delete(ctx, workspaceID, areaID, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.BindAreas(ctx, actor(ownerID), areaFilter(areaID)); !errors.Is(err, leadarea.ErrNotFound) {
		t.Fatalf("a deleted area = %v, want ErrNotFound", err)
	}

	plain := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsTrue}}}}}
	calls := len(areas.findSeen)
	if out, stamps, err := service(t, permissions{"leads:read": true}, areas).BindAreas(ctx, actor(ownerID), plain); err != nil || len(stamps) != 0 || len(out.Groups) != 1 {
		t.Fatalf("a filter without areas passes untouched for anyone: %+v %v", out, err)
	}
	if len(areas.findSeen) != calls {
		t.Fatal("a filter without areas reads no areas")
	}
}

func TestBindAreasRefusesWithoutTheAddressPermissionAndOnReadErrors(t *testing.T) {
	areas := newMemoryAreas(storedArea(ownerID, shared.VisibilityShared))
	ctx := context.Background()
	if _, _, err := service(t, permissions{"leads:read": true}, areas).BindAreas(ctx, actor(ownerID), areaFilter(areaID)); !errors.Is(err, leadarea.ErrAddressesRequired) {
		t.Fatalf("an area filter without leads:read_addresses = %v, want ErrForbidden: membership is an address oracle", err)
	}
	areas.findErr = errors.New("db down")
	if _, _, err := service(t, mapper, areas).BindAreas(ctx, actor(ownerID), areaFilter(areaID)); err == nil {
		t.Fatal("a failed area read must refuse the filter")
	}
	if _, _, err := service(t, mapper, areas).BindAreas(ctx, conversation.Viewer{WorkspaceID: workspaceID}, areaFilter(areaID)); !errors.Is(err, leadarea.ErrAddressesRequired) {
		t.Fatalf("an anonymous actor = %v", err)
	}
}

func TestCreateRefusesOnceTheWorkspaceHoldsAsManyAreasAsItCan(t *testing.T) {
	areas := newMemoryAreas()
	areas.extraLive = leadarea.MaxPerWorkspace
	if _, err := service(t, mapper, areas).Create(context.Background(), actor(ownerID), leadarea.Draft{Name: "A", Shape: rectangle()}); !errors.Is(err, leadarea.ErrLimitReached) {
		t.Fatalf("create past the cap = %v, want ErrLimitReached", err)
	}
	if len(areas.byID) != 0 {
		t.Fatal("a refused area is not stored")
	}
	areas.extraLive = 0
	areas.countErr = errors.New("db down")
	if _, err := service(t, mapper, areas).Create(context.Background(), actor(ownerID), leadarea.Draft{Name: "A", Shape: rectangle()}); err == nil || len(areas.byID) != 0 {
		t.Fatalf("an unreadable count = %v, want the create refused", err)
	}
}
