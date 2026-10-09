package lead

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/domain/leadarea"
	"vozko/domain/leadmap"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	leadarea_repository "vozko/infra/repositories/leadarea"
)

func exactAreas(t *testing.T, ws, viewer string, areas leadarea.Repository, ids ...string) crmfilter.Filter {
	t.Helper()
	f := boundToAreas(t, ws, viewer, areas, ids...)
	f.Groups[0].Predicates[0].Key = crmfilter.AreaExactOnly
	return f
}

func paulistaArea(t *testing.T, areas leadarea.Repository, ws, owner string) leadarea.Area {
	t.Helper()
	area, err := leadarea.New(ws, owner, leadarea.Draft{Name: "Paulista", Shape: geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -23.5614, Lng: -46.6559}, RadiusM: 1000}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	area.ID = uuid.NewString()
	if err := areas.Create(context.Background(), area); err != nil {
		t.Fatal(err)
	}
	return area
}

func TestAnAreaHoldsTheApproximatePositionsInsideItUnlessItAsksForExactOnesAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	ws, other := uuid.NewString(), uuid.NewString()
	owner := uuid.NewString()
	seedLead(t, db, ws, paulistaAddress("street"))
	seedLead(t, db, ws, paulistaAddress("district"))
	seedLead(t, db, ws, paulistaAddress("city"))
	farLat, farLng := at(-19.92, -43.94)
	seedLead(t, db, ws, &seededPosition{lat: farLat, lng: farLng, precision: "district"})
	seedLead(t, db, ws, &seededPosition{status: "pending", city: "São Paulo", cityKey: "3550308"})
	seedLead(t, db, ws, nil)
	seedLead(t, db, other, paulistaAddress("district"))

	areas := leadarea_repository.NewRepository(db)
	area := paulistaArea(t, areas, ws, owner)
	inclusive := boundToAreas(t, ws, owner, areas, area.ID)
	exact := exactAreas(t, ws, owner, areas, area.ID)
	reader := NewMapReader(db)
	houses, err := reader.Summary(context.Background(), leadmap.Scope{WorkspaceID: ws, Filter: exact})
	if err != nil {
		t.Fatal(err)
	}
	if houses.Total != 1 || houses.OnMap != 1 {
		t.Fatalf("exact only area = %+v, want the street lead only", houses)
	}
	both, err := reader.Summary(context.Background(), leadmap.Scope{WorkspaceID: ws, Filter: inclusive})
	if err != nil {
		t.Fatal(err)
	}
	if both.Total != 3 || both.OnMap != 1 || both.Approximate != 2 {
		t.Fatalf("area = %+v, want the street lead plus the bairro and city positions inside the ring", both)
	}
	if _, ok, err := leadarea.LeftOut(inclusive); ok || err != nil {
		t.Fatalf("an area that holds approximate positions leaves nothing out, got %v %v", ok, err)
	}
	left, ok, err := leadarea.LeftOut(exact)
	if err != nil || !ok {
		t.Fatalf("LeftOut() = %v, %v", ok, err)
	}
	approximate, err := reader.LeftOut(context.Background(), leadmap.Scope{WorkspaceID: ws, Filter: left}, leadmap.MaxLeftOutDistricts)
	if err != nil {
		t.Fatal(err)
	}
	if approximate.Total != both.Approximate || houses.Total+approximate.Total != both.Total {
		t.Fatalf("left out of the exact area = %d, want the %d approximate leads the default area holds", approximate.Total, both.Approximate)
	}
	w, _ := geo.SnapWindow(geo.BBox{South: -23.6, West: -46.7, North: -23.5, East: -46.6}, 13)
	people := 0
	for _, p := range layerIn(t, db, ws, inclusive, w).Points {
		people += len(p.LeadIDs)
	}
	if people != 3 {
		t.Fatalf("layer inside the area = %d people, want the three leads drawn", people)
	}
}

func TestAnAreaCountsTheSameLeadsInTheListTheSelectionTheSnapshotAndTheMapAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	if err := db.AutoMigrate(&schema.LeadSelectionSnapshot{}, &schema.LeadMemory{}, &schema.LeadMessageWindow{}, &schema.CallList{}); err != nil {
		t.Fatal(err)
	}
	ws, other := uuid.NewString(), uuid.NewString()
	owner := uuid.NewString()
	for _, precision := range []string{"exact", "street", "street", "postal_code", "district", "district", "city"} {
		seedLead(t, db, ws, paulistaAddress(precision))
	}
	farLat, farLng := at(-19.92, -43.94)
	seedLead(t, db, ws, &seededPosition{lat: farLat, lng: farLng, precision: "street"})
	seedLead(t, db, ws, &seededPosition{lat: farLat, lng: farLng, precision: "city"})
	seedLead(t, db, ws, &seededPosition{status: "pending", city: "São Paulo", cityKey: "3550308"})
	seedLead(t, db, ws, nil)
	seedLead(t, db, other, paulistaAddress("district"))

	areas := leadarea_repository.NewRepository(db)
	area := paulistaArea(t, areas, ws, owner)
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	reader := NewMapReader(db)
	ctx := context.Background()
	cases := []struct {
		name        string
		filter      crmfilter.Filter
		total       int
		approximate int
	}{
		{"default", boundToAreas(t, ws, owner, areas, area.ID), 7, 4},
		{"exact only", exactAreas(t, ws, owner, areas, area.ID), 3, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := repo.List(lead.ListLeadsInput{WorkspaceID: ws, Filter: tc.filter, Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 50}}})
			if err != nil {
				t.Fatal(err)
			}
			count, err := repo.CountSelection(ctx, lead.SelectionQuery{WorkspaceID: ws, Filter: tc.filter})
			if err != nil {
				t.Fatal(err)
			}
			frozen, err := repo.FreezeSelection(ctx, lead.SelectionQuery{WorkspaceID: ws, Filter: tc.filter}, uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			summary, err := reader.Summary(ctx, leadmap.Scope{WorkspaceID: ws, Filter: tc.filter})
			if err != nil {
				t.Fatal(err)
			}
			if int(page.TotalItems) != tc.total || len(page.Items) != tc.total || count != tc.total || frozen.Size != tc.total || int(summary.Total) != tc.total {
				t.Fatalf("list %d (%d rows), selection %d, snapshot %d, map %d; want %d everywhere", page.TotalItems, len(page.Items), count, frozen.Size, summary.Total, tc.total)
			}
			if int(summary.Approximate) != tc.approximate || int(summary.OnMap+summary.Approximate) != tc.total {
				t.Fatalf("map summary = %+v, want %d approximate of %d", summary, tc.approximate, tc.total)
			}
		})
	}
}
