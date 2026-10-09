package leadmap

import (
	"math"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
)

func AreaViewport(areas []crmfilter.AreaBounds, s Summary) (Viewport, bool) {
	var union *geo.BBox
	for _, a := range areas {
		box := geo.BBox{South: a.South, West: a.West, North: a.North, East: a.East}
		if box.Validate() != nil {
			continue
		}
		if union == nil {
			union = &box
			continue
		}
		union.South = math.Min(union.South, box.South)
		union.West = math.Min(union.West, box.West)
		union.North = math.Max(union.North, box.North)
		union.East = math.Max(union.East, box.East)
	}
	box, ok := padded(union)
	if !ok {
		return Viewport{}, false
	}
	return Viewport{BBox: box, Basis: BasisArea, View: s.DefaultView()}, true
}
