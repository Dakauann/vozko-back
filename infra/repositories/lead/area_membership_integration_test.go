package lead

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
	"vozko/domain/leadarea"
	"vozko/domain/leadmap"
	leadarea_repository "vozko/infra/repositories/leadarea"
)

func TestAnAreaWithApproximateLeadsAddsTheApproximatePositionsInsideItAgainstPostgres(t *testing.T) {
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
	area, err := leadarea.New(ws, owner, leadarea.Draft{Name: "Paulista", Shape: geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -23.5614, Lng: -46.6559}, RadiusM: 1000}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	area.ID = uuid.NewString()
	if err := areas.Create(context.Background(), area); err != nil {
		t.Fatal(err)
	}
	keyed := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldArea, Key: crmfilter.AreaWithApproximate, Operator: crmfilter.OpIn, Values: []string{area.ID}},
	}}}}
	found, err := areas.FindLive(context.Background(), ws, []string{area.ID})
	if err != nil {
		t.Fatal(err)
	}
	withApproximate, _, err := leadarea.Bind(keyed, ws, owner, found)
	if err != nil {
		t.Fatal(err)
	}
	reader := NewMapReader(db)
	houses, err := reader.Summary(context.Background(), leadmap.Scope{WorkspaceID: ws, Filter: boundToAreas(t, ws, owner, areas, area.ID)})
	if err != nil {
		t.Fatal(err)
	}
	if houses.Total != 1 || houses.OnMap != 1 {
		t.Fatalf("house precision area = %+v, want the street lead only", houses)
	}
	both, err := reader.Summary(context.Background(), leadmap.Scope{WorkspaceID: ws, Filter: withApproximate})
	if err != nil {
		t.Fatal(err)
	}
	if both.Total != 3 || both.OnMap != 1 || both.Approximate != 2 {
		t.Fatalf("area with approximate leads = %+v, want the street lead plus the bairro and city positions inside the ring", both)
	}
	left, ok, err := leadarea.LeftOut(withApproximate)
	if err != nil || !ok {
		t.Fatalf("LeftOut() = %v, %v", ok, err)
	}
	approximate, err := reader.LeftOut(context.Background(), leadmap.Scope{WorkspaceID: ws, Filter: left}, leadmap.MaxLeftOutDistricts)
	if err != nil {
		t.Fatal(err)
	}
	if approximate.Total != both.Approximate {
		t.Fatalf("approximate inside the area = %d, want the %d approximate leads the keyed area added", approximate.Total, both.Approximate)
	}
	w, _ := geo.SnapWindow(geo.BBox{South: -23.6, West: -46.7, North: -23.5, East: -46.6}, 13)
	layer := layerIn(t, db, ws, withApproximate, w)
	people := 0
	for _, p := range layer.Points {
		people += len(p.LeadIDs)
	}
	if people != 3 {
		t.Fatalf("layer inside the keyed area = %+v, want the three leads drawn", layer.Points)
	}
}
