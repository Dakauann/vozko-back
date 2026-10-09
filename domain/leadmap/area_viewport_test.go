package leadmap

import (
	"math"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
)

func TestADrawnAreaInTheFilterOpensTheMapOnTheUnionOfItsAreas(t *testing.T) {
	north := crmfilter.AreaBounds{ID: "north", South: -6.10, West: -35.70, North: -6.08, East: -35.68}
	south := crmfilter.AreaBounds{ID: "south", South: -6.20, West: -35.75, North: -6.15, East: -35.72}
	v, ok := AreaViewport([]crmfilter.AreaBounds{north, south}, Summary{})
	if !ok {
		t.Fatal("a filter with areas opens on them")
	}
	want := geo.BBox{South: -6.20, West: -35.75, North: -6.08, East: -35.68}
	if v.Basis != BasisArea || v.BBox != want {
		t.Fatalf("area viewport = %+v, want the union %+v", v, want)
	}
	if v.View != ViewPositions {
		t.Fatalf("an empty result keeps the positions view, got %q", v.View)
	}
	districts, _ := AreaViewport([]crmfilter.AreaBounds{north}, Summary{Total: 10, Approximate: 10})
	if districts.View != ViewDistricts {
		t.Fatalf("the view still follows the share of house positions, got %q", districts.View)
	}
}

func TestAnAreaViewportIgnoresUnreadableBoundsAndNeverOpensWithoutAnArea(t *testing.T) {
	if _, ok := AreaViewport(nil, Summary{}); ok {
		t.Fatal("no area, no area viewport")
	}
	broken := crmfilter.AreaBounds{ID: "broken", South: math.NaN(), West: -35.70, North: -6.08, East: -35.68}
	if _, ok := AreaViewport([]crmfilter.AreaBounds{broken}, Summary{}); ok {
		t.Fatal("an unreadable area never opens the map")
	}
	good := crmfilter.AreaBounds{ID: "good", South: -6.10, West: -35.70, North: -6.08, East: -35.68}
	v, ok := AreaViewport([]crmfilter.AreaBounds{broken, good}, Summary{})
	if !ok || v.BBox != (geo.BBox{South: -6.10, West: -35.70, North: -6.08, East: -35.68}) {
		t.Fatalf("the readable area still opens the map: %+v %v", v, ok)
	}
}

func TestAnAreaWithoutARingNeverOpensTheMapAtNullIsland(t *testing.T) {
	ringless := crmfilter.AreaBounds{ID: "ringless"}
	if v, ok := AreaViewport([]crmfilter.AreaBounds{ringless}, Summary{}); ok {
		t.Fatalf("an area without a ring opened %+v", v)
	}
}
