package geo

import (
	"errors"
	"math"
	"testing"
)

var paulistaSquare = []Point{
	{Lat: -23.60, Lng: -46.70},
	{Lat: -23.60, Lng: -46.60},
	{Lat: -23.50, Lng: -46.60},
	{Lat: -23.50, Lng: -46.70},
}

func regularRing(center Point, radiusDegrees float64, vertices int) []Point {
	ring := make([]Point, 0, vertices)
	for i := 0; i < vertices; i++ {
		angle := 2 * math.Pi * float64(i) / float64(vertices)
		ring = append(ring, Point{Lat: center.Lat + radiusDegrees*math.Cos(angle), Lng: center.Lng + radiusDegrees*math.Sin(angle)})
	}
	return ring
}

func closed(ring []Point) []Point {
	return append(append([]Point{}, ring...), ring[0])
}

func TestShapeValidate(t *testing.T) {
	center := Point{Lat: -23.55, Lng: -46.65}
	brasilia := Point{Lat: -15.79, Lng: -47.88}
	saoPauloState := []Point{{Lat: -25.3, Lng: -53.1}, {Lat: -25.3, Lng: -44.2}, {Lat: -19.8, Lng: -44.2}, {Lat: -19.8, Lng: -53.1}}
	country := []Point{{Lat: -33.8, Lng: -74.1}, {Lat: -33.8, Lng: -28.8}, {Lat: 5.3, Lng: -28.8}, {Lat: 5.3, Lng: -74.1}}
	tests := []struct {
		name     string
		shape    Shape
		expected error
	}{
		{"accepts a polygon", Shape{Kind: ShapePolygon, Ring: paulistaSquare}, nil},
		{"accepts a polygon whose ring repeats the first point at the end", Shape{Kind: ShapePolygon, Ring: closed(paulistaSquare)}, nil},
		{"accepts 200 vertices", Shape{Kind: ShapePolygon, Ring: regularRing(center, 0.05, MaxRingVertices)}, nil},
		{"accepts 200 vertices plus the closing point", Shape{Kind: ShapePolygon, Ring: closed(regularRing(center, 0.05, MaxRingVertices))}, nil},
		{"accepts a polygon the size of a state", Shape{Kind: ShapePolygon, Ring: saoPauloState}, nil},
		{"accepts a polygon around the whole country", Shape{Kind: ShapePolygon, Ring: country}, nil},
		{"accepts a sloppy corner drawn just past the border", Shape{Kind: ShapePolygon, Ring: []Point{{Lat: -35.5, Lng: -57}, {Lat: -30, Lng: -49}, {Lat: -27, Lng: -55}}}, nil},
		{"refuses 201 vertices", Shape{Kind: ShapePolygon, Ring: regularRing(center, 0.05, MaxRingVertices+1)}, ErrTooManyVertices},
		{"refuses fewer than 3 vertices", Shape{Kind: ShapePolygon, Ring: paulistaSquare[:2]}, ErrInvalidShape},
		{"refuses a self-crossing bowtie", Shape{Kind: ShapePolygon, Ring: []Point{paulistaSquare[0], paulistaSquare[2], paulistaSquare[1], paulistaSquare[3]}}, ErrSelfCrossing},
		{"refuses collinear vertices that enclose nothing", Shape{Kind: ShapePolygon, Ring: []Point{{Lat: -23.5, Lng: -46.7}, {Lat: -23.5, Lng: -46.6}, {Lat: -23.5, Lng: -46.5}}}, ErrEmptyShape},
		{"refuses four collinear vertices as empty, not crossing", Shape{Kind: ShapePolygon, Ring: []Point{{Lat: -23.5, Lng: -46.7}, {Lat: -23.4, Lng: -46.6}, {Lat: -23.3, Lng: -46.5}, {Lat: -23.2, Lng: -46.4}}}, ErrEmptyShape},
		{"refuses a ring that repeats one point", Shape{Kind: ShapePolygon, Ring: []Point{center, center, center, center}}, ErrEmptyShape},
		{"refuses a vertex outside the map of Brazil", Shape{Kind: ShapePolygon, Ring: []Point{{Lat: -23.6, Lng: -46.7}, {Lat: 38.72, Lng: -9.14}, {Lat: -23.5, Lng: -46.6}}}, ErrShapeOutsideBounds},
		{"refuses an invalid vertex", Shape{Kind: ShapePolygon, Ring: []Point{{Lat: -23.6, Lng: -46.7}, {Lat: math.NaN(), Lng: -46.6}, {Lat: -23.5, Lng: -46.6}}}, ErrInvalidPoint},
		{"refuses a polygon that also carries a radius", Shape{Kind: ShapePolygon, Ring: paulistaSquare, RadiusM: 100}, ErrInvalidShape},
		{"refuses an unknown kind", Shape{Kind: "hexagon", Ring: paulistaSquare}, ErrUnknownShapeKind},
		{"refuses an empty kind", Shape{Ring: paulistaSquare}, ErrUnknownShapeKind},
		{"accepts an axis-aligned rectangle", Shape{Kind: ShapeRectangle, Ring: paulistaSquare}, nil},
		{"accepts a closed rectangle", Shape{Kind: ShapeRectangle, Ring: closed(paulistaSquare)}, nil},
		{"accepts a rectangle around the whole country", Shape{Kind: ShapeRectangle, Ring: country}, nil},
		{"accepts a rectangle whose corners carry float noise", Shape{Kind: ShapeRectangle, Ring: noisyPaulistaSquare}, nil},
		{"refuses a rectangle that is not axis aligned", Shape{Kind: ShapeRectangle, Ring: []Point{{Lat: -23.6, Lng: -46.7}, {Lat: -23.6, Lng: -46.6}, {Lat: -23.5, Lng: -46.55}, {Lat: -23.5, Lng: -46.7}}}, ErrInvalidShape},
		{"refuses a rectangle with five corners", Shape{Kind: ShapeRectangle, Ring: regularRing(center, 0.05, 5)}, ErrInvalidShape},
		{"refuses a rectangle with no width", Shape{Kind: ShapeRectangle, Ring: []Point{{Lat: -23.6, Lng: -46.7}, {Lat: -23.6, Lng: -46.7}, {Lat: -23.5, Lng: -46.7}, {Lat: -23.5, Lng: -46.7}}}, ErrEmptyShape},
		{"refuses a rectangle whose corners cross", Shape{Kind: ShapeRectangle, Ring: []Point{paulistaSquare[0], paulistaSquare[2], paulistaSquare[1], paulistaSquare[3]}}, ErrSelfCrossing},
		{"accepts a circle", Shape{Kind: ShapeCircle, Center: center, RadiusM: 1000}, nil},
		{"accepts a circle of 300 km", Shape{Kind: ShapeCircle, Center: center, RadiusM: 300000}, nil},
		{"accepts a circle of 1,000 km in the middle of the country", Shape{Kind: ShapeCircle, Center: brasilia, RadiusM: 1000000}, nil},
		{"refuses a circle that reaches past the map of Brazil", Shape{Kind: ShapeCircle, Center: brasilia, RadiusM: 3000000}, ErrShapeOutsideBounds},
		{"refuses a circle centred outside the map of Brazil", Shape{Kind: ShapeCircle, Center: Point{Lat: 38.72, Lng: -9.14}, RadiusM: 1000}, ErrShapeOutsideBounds},
		{"refuses a radius larger than the earth", Shape{Kind: ShapeCircle, Center: brasilia, RadiusM: 1e12}, ErrShapeOutsideBounds},
		{"refuses a zero radius as empty", Shape{Kind: ShapeCircle, Center: center}, ErrEmptyShape},
		{"refuses a negative radius", Shape{Kind: ShapeCircle, Center: center, RadiusM: -10}, ErrInvalidRadius},
		{"refuses a NaN radius", Shape{Kind: ShapeCircle, Center: center, RadiusM: math.NaN()}, ErrInvalidRadius},
		{"refuses an infinite radius", Shape{Kind: ShapeCircle, Center: center, RadiusM: math.Inf(1)}, ErrInvalidRadius},
		{"refuses a circle without a center", Shape{Kind: ShapeCircle, RadiusM: 1000}, ErrInvalidPoint},
		{"refuses a circle that also carries a ring", Shape{Kind: ShapeCircle, Center: center, RadiusM: 1000, Ring: paulistaSquare}, ErrInvalidShape},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.shape.Validate()
			if tt.expected == nil && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if tt.expected != nil && !errors.Is(err, tt.expected) {
				t.Fatalf("expected %v, got %v", tt.expected, err)
			}
		})
	}
}

var noisyPaulistaSquare = []Point{
	{Lat: -23.6000000001, Lng: -46.70},
	{Lat: -23.60, Lng: -46.5999999998},
	{Lat: -23.4999999997, Lng: -46.60},
	{Lat: -23.50, Lng: -46.7000000002},
}

func TestARectangleSnapsItsCornersToItsBoundingBox(t *testing.T) {
	tests := []struct {
		name     string
		ring     []Point
		expected []Point
	}{
		{"exact corners stay as they are", paulistaSquare, paulistaSquare},
		{"float noise snaps to the box corners, in the drawn order", noisyPaulistaSquare, []Point{{Lat: -23.6000000001, Lng: -46.7000000002}, {Lat: -23.6000000001, Lng: -46.5999999998}, {Lat: -23.4999999997, Lng: -46.5999999998}, {Lat: -23.4999999997, Lng: -46.7000000002}}},
		{"the closing point is dropped", closed(paulistaSquare), paulistaSquare},
		{"a corner within the tolerance of the shorter side snaps", []Point{{Lat: -23.60, Lng: -46.70}, {Lat: -23.60009, Lng: -46.60}, {Lat: -23.50, Lng: -46.60}, {Lat: -23.50, Lng: -46.70}}, []Point{{Lat: -23.60009, Lng: -46.70}, {Lat: -23.60009, Lng: -46.60}, {Lat: -23.50, Lng: -46.60}, {Lat: -23.50, Lng: -46.70}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized, err := Shape{Kind: ShapeRectangle, Ring: tt.ring}.Normalized()
			if err != nil {
				t.Fatal(err)
			}
			if len(normalized.Ring) != len(tt.expected) {
				t.Fatalf("ring = %v, expected %v", normalized.Ring, tt.expected)
			}
			ring := normalized.Ring64()
			for i := range tt.expected {
				if normalized.Ring[i] != tt.expected[i] || ring[i] != tt.expected[i] {
					t.Fatalf("corner %d = %v (Ring64 %v), expected %v", i, normalized.Ring[i], ring[i], tt.expected[i])
				}
			}
		})
	}
	if _, err := (Shape{Kind: ShapeRectangle, Ring: []Point{{Lat: -23.60, Lng: -46.70}, {Lat: -23.6002, Lng: -46.60}, {Lat: -23.50, Lng: -46.60}, {Lat: -23.50, Lng: -46.70}}}).Normalized(); !errors.Is(err, ErrInvalidShape) {
		t.Fatalf("a corner past the tolerance = %v, expected ErrInvalidShape", err)
	}
}

func TestNormalizedLeavesPolygonsAndCirclesAsDrawn(t *testing.T) {
	polygon := Shape{Kind: ShapePolygon, Ring: closed(paulistaSquare)}
	normalized, err := polygon.Normalized()
	if err != nil || len(normalized.Ring) != len(polygon.Ring) {
		t.Fatalf("polygon = %+v, %v", normalized, err)
	}
	circle := Shape{Kind: ShapeCircle, Center: Point{Lat: -23.55, Lng: -46.65}, RadiusM: 300000}
	if normalized, err := circle.Normalized(); err != nil || normalized.Center != circle.Center || normalized.RadiusM != circle.RadiusM {
		t.Fatalf("circle = %+v, %v", normalized, err)
	}
	if _, err := (Shape{Kind: ShapePolygon}).Normalized(); !errors.Is(err, ErrInvalidShape) {
		t.Fatalf("an invalid shape = %v, expected its refusal", err)
	}
}

func TestShapeBoundsHoldBrazilWithAMargin(t *testing.T) {
	bounds, brazil := ShapeBounds(), BrazilBounds()
	if bounds.Validate() != nil {
		t.Fatalf("shape bounds = %+v", bounds)
	}
	if bounds.South >= brazil.South || bounds.West >= brazil.West || bounds.North <= brazil.North || bounds.East <= brazil.East {
		t.Fatalf("shape bounds %+v must hold Brazil %+v with a margin", bounds, brazil)
	}
	if bounds.contains(Point{Lat: 38.72, Lng: -9.14}) || bounds.contains(Point{Lat: 40.7, Lng: -74}) {
		t.Fatalf("shape bounds %+v reach other continents", bounds)
	}
}

func TestShapeContains(t *testing.T) {
	center := Point{Lat: -23.55, Lng: -46.65}
	circle := Shape{Kind: ShapeCircle, Center: center, RadiusM: 2000}
	north := func(meters float64) Point {
		return Point{Lat: center.Lat + meters/metersPerDegreeLat, Lng: center.Lng}
	}
	east := func(meters float64) Point {
		return Point{Lat: center.Lat, Lng: center.Lng + meters/(metersPerDegreeLat*math.Cos(center.Lat*math.Pi/180))}
	}
	tests := []struct {
		name     string
		shape    Shape
		point    Point
		expected bool
	}{
		{"a point inside a polygon", Shape{Kind: ShapePolygon, Ring: paulistaSquare}, center, true},
		{"a point outside a polygon", Shape{Kind: ShapePolygon, Ring: paulistaSquare}, Point{Lat: -23.40, Lng: -46.65}, false},
		{"a point inside the notch of a concave polygon is outside", Shape{Kind: ShapePolygon, Ring: []Point{{Lat: -23.6, Lng: -46.7}, {Lat: -23.6, Lng: -46.6}, {Lat: -23.5, Lng: -46.6}, {Lat: -23.58, Lng: -46.65}, {Lat: -23.5, Lng: -46.7}}}, Point{Lat: -23.52, Lng: -46.65}, false},
		{"a point inside a rectangle", Shape{Kind: ShapeRectangle, Ring: paulistaSquare}, center, true},
		{"a point 1.8 km north of a 2 km circle center", circle, north(1800), true},
		{"a point 2.2 km north of a 2 km circle center", circle, north(2200), false},
		{"a point 1.8 km east of a 2 km circle center", circle, east(1800), true},
		{"a point 2.2 km east of a 2 km circle center", circle, east(2200), false},
		{"an invalid shape contains nothing", Shape{Kind: ShapePolygon, Ring: []Point{paulistaSquare[0], paulistaSquare[2], paulistaSquare[1], paulistaSquare[3]}}, center, false},
		{"an invalid point is in no shape", Shape{Kind: ShapePolygon, Ring: paulistaSquare}, Point{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.shape.Contains(tt.point); got != tt.expected {
				t.Fatalf("Contains() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestRing64OfACircleStaysOnTheRadiusAtEveryLatitude(t *testing.T) {
	for _, lat := range []float64{-3.7, -23.55, -33.6} {
		center := Point{Lat: lat, Lng: -50}
		for _, radius := range []float64{150, 5000, 50000, 300000} {
			ring := Shape{Kind: ShapeCircle, Center: center, RadiusM: radius}.Ring64()
			if len(ring) != CircleVertices {
				t.Fatalf("lat %v radius %v: %d vertices, expected %d", lat, radius, len(ring), CircleVertices)
			}
			for i, vertex := range ring {
				if d := DistanceMeters(center, vertex); math.Abs(d-radius)/radius > 1e-9 {
					t.Fatalf("lat %v radius %v vertex %d is %.1f m away", lat, radius, i, d)
				}
			}
		}
	}
}

func TestRing64OfAPolygonIsItsOpenRing(t *testing.T) {
	ring := Shape{Kind: ShapePolygon, Ring: closed(paulistaSquare)}.Ring64()
	if len(ring) != len(paulistaSquare) {
		t.Fatalf("expected %d vertices, got %d", len(paulistaSquare), len(ring))
	}
	ring[0] = Point{Lat: 1, Lng: 1}
	if paulistaSquare[0].Lat == 1 {
		t.Fatal("Ring64 must return a copy")
	}
}

func TestRing64OfAnInvalidShapeIsEmpty(t *testing.T) {
	if ring := (Shape{Kind: ShapeCircle, Center: Point{Lat: -23, Lng: -46}}).Ring64(); ring != nil {
		t.Fatalf("expected no ring for a circle without radius, got %d vertices", len(ring))
	}
}
