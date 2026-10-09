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
	tests := []struct {
		name     string
		shape    Shape
		expected error
	}{
		{"accepts a polygon", Shape{Kind: ShapePolygon, Ring: paulistaSquare}, nil},
		{"accepts a polygon whose ring repeats the first point at the end", Shape{Kind: ShapePolygon, Ring: closed(paulistaSquare)}, nil},
		{"accepts 200 vertices", Shape{Kind: ShapePolygon, Ring: regularRing(center, 0.05, MaxRingVertices)}, nil},
		{"accepts 200 vertices plus the closing point", Shape{Kind: ShapePolygon, Ring: closed(regularRing(center, 0.05, MaxRingVertices))}, nil},
		{"refuses 201 vertices", Shape{Kind: ShapePolygon, Ring: regularRing(center, 0.05, MaxRingVertices+1)}, ErrTooManyVertices},
		{"refuses fewer than 3 vertices", Shape{Kind: ShapePolygon, Ring: paulistaSquare[:2]}, ErrInvalidShape},
		{"refuses a self-crossing bowtie", Shape{Kind: ShapePolygon, Ring: []Point{paulistaSquare[0], paulistaSquare[2], paulistaSquare[1], paulistaSquare[3]}}, ErrSelfCrossing},
		{"refuses collinear vertices that enclose nothing", Shape{Kind: ShapePolygon, Ring: []Point{{Lat: -23.5, Lng: -46.7}, {Lat: -23.5, Lng: -46.6}, {Lat: -23.5, Lng: -46.5}}}, ErrInvalidShape},
		{"refuses a polygon wider than a state", Shape{Kind: ShapePolygon, Ring: []Point{{Lat: -20, Lng: -55}, {Lat: -20, Lng: -40}, {Lat: -10, Lng: -40}, {Lat: -10, Lng: -55}}}, ErrShapeTooWide},
		{"refuses an invalid vertex", Shape{Kind: ShapePolygon, Ring: []Point{{Lat: -23.6, Lng: -46.7}, {Lat: math.NaN(), Lng: -46.6}, {Lat: -23.5, Lng: -46.6}}}, ErrInvalidPoint},
		{"refuses a polygon that also carries a radius", Shape{Kind: ShapePolygon, Ring: paulistaSquare, RadiusM: 100}, ErrInvalidShape},
		{"refuses an unknown kind", Shape{Kind: "hexagon", Ring: paulistaSquare}, ErrUnknownShapeKind},
		{"refuses an empty kind", Shape{Ring: paulistaSquare}, ErrUnknownShapeKind},
		{"accepts an axis-aligned rectangle", Shape{Kind: ShapeRectangle, Ring: paulistaSquare}, nil},
		{"accepts a closed rectangle", Shape{Kind: ShapeRectangle, Ring: closed(paulistaSquare)}, nil},
		{"refuses a rectangle that is not axis aligned", Shape{Kind: ShapeRectangle, Ring: []Point{{Lat: -23.6, Lng: -46.7}, {Lat: -23.6, Lng: -46.6}, {Lat: -23.5, Lng: -46.55}, {Lat: -23.5, Lng: -46.7}}}, ErrInvalidShape},
		{"refuses a rectangle with five corners", Shape{Kind: ShapeRectangle, Ring: regularRing(center, 0.05, 5)}, ErrInvalidShape},
		{"accepts a circle", Shape{Kind: ShapeCircle, Center: center, RadiusM: 1000}, nil},
		{"accepts a circle of exactly 50 km", Shape{Kind: ShapeCircle, Center: center, RadiusM: MaxCircleRadiusM}, nil},
		{"refuses a circle above 50 km", Shape{Kind: ShapeCircle, Center: center, RadiusM: MaxCircleRadiusM + 1}, ErrInvalidRadius},
		{"refuses a zero radius", Shape{Kind: ShapeCircle, Center: center}, ErrInvalidRadius},
		{"refuses a negative radius", Shape{Kind: ShapeCircle, Center: center, RadiusM: -10}, ErrInvalidRadius},
		{"refuses a NaN radius", Shape{Kind: ShapeCircle, Center: center, RadiusM: math.NaN()}, ErrInvalidRadius},
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
		for _, radius := range []float64{150, 5000, MaxCircleRadiusM} {
			ring := Shape{Kind: ShapeCircle, Center: center, RadiusM: radius}.Ring64()
			if len(ring) != CircleVertices {
				t.Fatalf("lat %v radius %v: %d vertices, expected %d", lat, radius, len(ring), CircleVertices)
			}
			for i, vertex := range ring {
				if d := DistanceMeters(center, vertex); math.Abs(d-radius)/radius > 0.005 {
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
