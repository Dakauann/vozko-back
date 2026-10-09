package lead_usecase

import (
	"context"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
	"vozko/domain/leadmap"
)

func TestTheViewportOpensOnTheDrawnAreaEvenWhenNobodyIsInside(t *testing.T) {
	f := newMapFixture()
	f.areas.shape = geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -6.31, Lng: -35.47}, RadiusM: 1000}
	f.reader.summary = leadmap.Summary{}
	f.reader.extent = &geo.BBox{South: -30, West: -60, North: 0, East: -35}
	v, err := f.sections(t, mapViewer).Viewport(context.Background(), operator(), areaOnly())
	if err != nil {
		t.Fatal(err)
	}
	if v.Basis != leadmap.BasisArea {
		t.Fatalf("viewport = %+v, want the area basis", v)
	}
	if v.BBox.South > -6.31 || v.BBox.North < -6.31 || v.BBox.West > -35.47 || v.BBox.East < -35.47 || v.BBox.North-v.BBox.South > 0.1 {
		t.Fatalf("viewport box %+v must frame the 1 km circle", v.BBox)
	}
	if len(f.reader.scopes) != 1 {
		t.Fatalf("an area viewport reads only the summary, got %d reads", len(f.reader.scopes))
	}
}

func TestTheViewportWithoutAnAreaStillFollowsTheLeads(t *testing.T) {
	f := newMapFixture()
	f.reader.summary = leadmap.Summary{Total: 10, OnMap: 9}
	f.reader.extent = &geo.BBox{South: -23.6, West: -46.7, North: -23.5, East: -46.6}
	v, err := f.sections(t, mapViewer).Viewport(context.Background(), operator(), crmfilter.Filter{})
	if err != nil || v.Basis != leadmap.BasisLocated {
		t.Fatalf("viewport = %+v %v", v, err)
	}
}
