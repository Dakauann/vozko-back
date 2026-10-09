package leadarea_repository

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/geo"
	"vozko/domain/leadarea"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestTheStoredRingHoldsASaoPauloPointInLongitudeLatitudeOrderAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_area_ring", &schema.LeadArea{})
	repo := NewRepository(db)
	ctx := context.Background()
	a := square()
	a.ID, a.WorkspaceID = uuid.NewString(), uuid.NewString()
	if err := repo.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	var inside, swapped bool
	if err := db.Raw(`SELECT point(-46.6559, -23.5614) <@ ring, point(-23.5614, -46.6559) <@ ring FROM lead_areas WHERE id = ?`, a.ID).Row().Scan(&inside, &swapped); err != nil {
		t.Fatal(err)
	}
	if !inside || swapped {
		t.Fatalf("Avenida Paulista as point(lng, lat) inside = %v, swapped inside = %v", inside, swapped)
	}

	circle := a
	circle.ID = uuid.NewString()
	circle.Shape = geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -23.5614, Lng: -46.6559}, RadiusM: 500}
	if err := repo.Create(ctx, circle); err != nil {
		t.Fatal(err)
	}
	var vertices int
	if err := db.Raw(`SELECT npoints(ring) FROM lead_areas WHERE id = ?`, circle.ID).Scan(&vertices).Error; err != nil || vertices != geo.CircleVertices {
		t.Fatalf("a circle is stored as %d vertices, %v", vertices, err)
	}
	got, err := repo.Get(ctx, circle.WorkspaceID, circle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Shape.Kind != geo.ShapeCircle || got.Shape.RadiusM != 500 || got.Shape.Center != circle.Shape.Center {
		t.Fatalf("the shape the person drew comes back as drawn: %+v", got.Shape)
	}
}

func TestAReshapedAreaMovesItsRingAndADeletedOneIsGoneAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_area_edit", &schema.LeadArea{})
	repo := NewRepository(db)
	ctx := context.Background()
	a := square()
	a.ID, a.WorkspaceID = uuid.NewString(), uuid.NewString()
	if err := repo.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	moved := a
	moved.Name = "Belo Horizonte"
	moved.Shape = geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -19.92, Lng: -43.94}, RadiusM: 2000}
	moved.UpdatedAt = at.Add(5 * time.Minute)
	if err := repo.Update(ctx, moved); err != nil {
		t.Fatal(err)
	}
	var inBH, inSP bool
	if err := db.Raw(`SELECT point(-43.94, -19.92) <@ ring, point(-46.6559, -23.5614) <@ ring FROM lead_areas WHERE id = ?`, a.ID).Row().Scan(&inBH, &inSP); err != nil {
		t.Fatal(err)
	}
	if !inBH || inSP {
		t.Fatalf("the ring follows the new shape: BH %v, SP %v", inBH, inSP)
	}
	if err := repo.Delete(ctx, a.WorkspaceID, a.ID, at.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, a.WorkspaceID, a.ID); !errors.Is(err, leadarea.ErrNotFound) {
		t.Fatalf("a deleted area = %v, want ErrNotFound", err)
	}
	if found, err := repo.FindLive(ctx, a.WorkspaceID, []string{a.ID}); err != nil || len(found) != 0 {
		t.Fatalf("a deleted area never resolves for a filter: %v %v", found, err)
	}
	if err := repo.Delete(ctx, a.WorkspaceID, a.ID, at); !errors.Is(err, leadarea.ErrNotFound) {
		t.Fatalf("deleting twice = %v, want ErrNotFound", err)
	}
}

func TestListReadableMatchesTheSharedReadRuleAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_area_read", &schema.LeadArea{})
	repo := NewRepository(db)
	ctx := context.Background()
	workspace, elsewhere := uuid.NewString(), uuid.NewString()
	ownerA, ownerB, outsider := uuid.NewString(), uuid.NewString(), uuid.NewString()
	var seeded []leadarea.Area
	for _, o := range []string{ownerA, ownerB} {
		for _, v := range []shared.Visibility{shared.VisibilityPrivate, shared.VisibilityShared} {
			a := square()
			a.ID, a.WorkspaceID, a.OwnerID, a.Visibility, a.Name = uuid.NewString(), workspace, o, v, string(v)+o[:4]
			if err := repo.Create(ctx, a); err != nil {
				t.Fatal(err)
			}
			seeded = append(seeded, a)
		}
	}
	foreign := square()
	foreign.ID, foreign.WorkspaceID, foreign.OwnerID = uuid.NewString(), elsewhere, ownerA
	if err := repo.Create(ctx, foreign); err != nil {
		t.Fatal(err)
	}
	for _, viewer := range []string{ownerA, ownerB, outsider} {
		listed, err := repo.ListReadable(ctx, workspace, viewer)
		if err != nil {
			t.Fatal(err)
		}
		var want, got []string
		for _, a := range seeded {
			if a.Owned().CanRead(viewer) {
				want = append(want, a.ID)
			}
		}
		for _, a := range listed {
			got = append(got, a.ID)
		}
		sort.Strings(want)
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("viewer %s: the list drifted from Owned.CanRead:\n got %v\nwant %v", viewer, got, want)
		}
	}
	if found, err := repo.FindLive(ctx, workspace, []string{foreign.ID}); err != nil || len(found) != 0 {
		t.Fatalf("another workspace's area never resolves: %v %v", found, err)
	}
}
