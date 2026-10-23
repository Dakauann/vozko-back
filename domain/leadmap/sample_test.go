package leadmap

import (
	"errors"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
)

func TestASampleIsSampledOnlyWhenPositionsWereLeftOut(t *testing.T) {
	one := Point{Position: geo.Point{Lat: -23.5, Lng: -46.6}}
	if (Sample{Points: []Point{one}, Positions: 1}).Sampled() {
		t.Fatal("every position is shown")
	}
	if !(Sample{Points: []Point{one}, Positions: 2}).Sampled() {
		t.Fatal("a position was left out")
	}
}

func TestTheSampleLimitIsBounded(t *testing.T) {
	for _, limit := range []int{0, -1, MaxSamplePoints + 1} {
		if err := CheckSampleLimit(limit); !errors.Is(err, ErrSampleLimit) {
			t.Fatalf("limit %d was accepted", limit)
		}
	}
	for _, limit := range []int{1, MaxSamplePoints} {
		if err := CheckSampleLimit(limit); err != nil {
			t.Fatalf("limit %d was refused: %v", limit, err)
		}
	}
}

func TestTheAreaBoxJoinsTheBoundAreasAndIsAbsentWithoutOne(t *testing.T) {
	if AreaBox(nil) != nil {
		t.Fatal("no area, no box")
	}
	north := crmfilter.AreaBounds{ID: "north", South: -6.10, West: -35.70, North: -6.08, East: -35.68}
	south := crmfilter.AreaBounds{ID: "south", South: -6.20, West: -35.75, North: -6.15, East: -35.72}
	box := AreaBox([]crmfilter.AreaBounds{north, south})
	want := geo.BBox{South: -6.20, West: -35.75, North: -6.08, East: -35.68}
	if box == nil || *box != want {
		t.Fatalf("area box = %+v, want %+v", box, want)
	}
}
