package geo

import (
	"errors"
	"fmt"
	"math"
)

var (
	ErrUnknownShapeKind = errors.New("geo: unknown shape kind")
	ErrInvalidShape     = errors.New("geo: invalid shape")
	ErrTooManyVertices  = errors.New("geo: too many vertices")
	ErrSelfCrossing     = errors.New("geo: ring crosses itself")
	ErrShapeTooWide     = errors.New("geo: shape is wider than a state")
	ErrInvalidRadius    = errors.New("geo: invalid radius")
)

const (
	MaxRingVertices  = 200
	CircleVertices   = 64
	MaxCircleRadiusM = 50000
	MaxShapeSpanM    = 1000000
	minRingArea      = 1e-12
)

type ShapeKind string

const (
	ShapePolygon   ShapeKind = "polygon"
	ShapeRectangle ShapeKind = "rectangle"
	ShapeCircle    ShapeKind = "circle"
)

type Shape struct {
	Kind    ShapeKind
	Ring    []Point
	Center  Point
	RadiusM float64
}

func (s Shape) Validate() error {
	switch s.Kind {
	case ShapePolygon:
		return s.validateRing()
	case ShapeRectangle:
		if err := s.validateRing(); err != nil {
			return err
		}
		if !axisAligned(openRing(s.Ring)) {
			return fmt.Errorf("%w: a rectangle needs four axis-aligned corners", ErrInvalidShape)
		}
		return nil
	case ShapeCircle:
		return s.validateCircle()
	}
	return ErrUnknownShapeKind
}

func (s Shape) Contains(p Point) bool {
	if p.Validate() != nil {
		return false
	}
	ring := s.Ring64()
	if ring == nil {
		return false
	}
	return ringContains(ring, p)
}

func (s Shape) Ring64() []Point {
	if s.Validate() != nil {
		return nil
	}
	if s.Kind == ShapeCircle {
		return circleRing(s.Center, s.RadiusM)
	}
	return append([]Point(nil), openRing(s.Ring)...)
}

func (s Shape) validateRing() error {
	if s.RadiusM != 0 || s.Center != (Point{}) {
		return fmt.Errorf("%w: a ring shape carries no center or radius", ErrInvalidShape)
	}
	ring := openRing(s.Ring)
	if len(ring) < 3 {
		return fmt.Errorf("%w: a ring needs at least 3 vertices", ErrInvalidShape)
	}
	if len(ring) > MaxRingVertices {
		return ErrTooManyVertices
	}
	for i, vertex := range ring {
		if err := vertex.Validate(); err != nil {
			return fmt.Errorf("%w: vertex %d", err, i)
		}
	}
	if selfCrossing(ring) {
		return ErrSelfCrossing
	}
	if math.Abs(signedArea(ring)) < minRingArea {
		return fmt.Errorf("%w: the ring encloses no area", ErrInvalidShape)
	}
	if spanMeters(ring) > MaxShapeSpanM {
		return ErrShapeTooWide
	}
	return nil
}

func (s Shape) validateCircle() error {
	if len(s.Ring) != 0 {
		return fmt.Errorf("%w: a circle carries no ring", ErrInvalidShape)
	}
	if err := s.Center.Validate(); err != nil {
		return fmt.Errorf("%w: circle center", err)
	}
	if !finite(s.RadiusM) || s.RadiusM <= 0 || s.RadiusM > MaxCircleRadiusM {
		return ErrInvalidRadius
	}
	return nil
}

func openRing(ring []Point) []Point {
	if len(ring) > 1 && ring[0] == ring[len(ring)-1] {
		return ring[:len(ring)-1]
	}
	return ring
}

func axisAligned(ring []Point) bool {
	if len(ring) != 4 {
		return false
	}
	for i := range ring {
		a, b := ring[i], ring[(i+1)%len(ring)]
		if (a.Lat == b.Lat) == (a.Lng == b.Lng) {
			return false
		}
	}
	return true
}

func circleRing(center Point, radiusM float64) []Point {
	ring := make([]Point, 0, CircleVertices)
	for i := 0; i < CircleVertices; i++ {
		angle := 2 * math.Pi * float64(i) / CircleVertices
		lat := center.Lat + radiusM*math.Cos(angle)/metersPerDegreeLat
		lng := center.Lng + radiusM*math.Sin(angle)/(metersPerDegreeLat*math.Cos(radians(lat)))
		ring = append(ring, Point{Lat: lat, Lng: lng})
	}
	return ring
}

func ringContains(ring []Point, p Point) bool {
	inside := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		a, b := ring[i], ring[j]
		if onSegment(a, b, p) {
			return true
		}
		if (a.Lat > p.Lat) != (b.Lat > p.Lat) {
			crossLng := a.Lng + (p.Lat-a.Lat)*(b.Lng-a.Lng)/(b.Lat-a.Lat)
			if p.Lng < crossLng {
				inside = !inside
			}
		}
	}
	return inside
}

func selfCrossing(ring []Point) bool {
	n := len(ring)
	for i := 0; i < n; i++ {
		a1, a2 := ring[i], ring[(i+1)%n]
		for j := i + 1; j < n; j++ {
			if j == i+1 || (i == 0 && j == n-1) {
				continue
			}
			if segmentsTouch(a1, a2, ring[j], ring[(j+1)%n]) {
				return true
			}
		}
	}
	return false
}

func segmentsTouch(p1, p2, q1, q2 Point) bool {
	d1 := orientation(q1, q2, p1)
	d2 := orientation(q1, q2, p2)
	d3 := orientation(p1, p2, q1)
	d4 := orientation(p1, p2, q2)
	if ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) && ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0)) {
		return true
	}
	return onSegment(q1, q2, p1) || onSegment(q1, q2, p2) || onSegment(p1, p2, q1) || onSegment(p1, p2, q2)
}

func orientation(a, b, c Point) float64 {
	return (b.Lng-a.Lng)*(c.Lat-a.Lat) - (b.Lat-a.Lat)*(c.Lng-a.Lng)
}

func onSegment(a, b, p Point) bool {
	if math.Abs(orientation(a, b, p)) > minRingArea {
		return false
	}
	return p.Lng >= math.Min(a.Lng, b.Lng) && p.Lng <= math.Max(a.Lng, b.Lng) &&
		p.Lat >= math.Min(a.Lat, b.Lat) && p.Lat <= math.Max(a.Lat, b.Lat)
}

func signedArea(ring []Point) float64 {
	area := 0.0
	for i := range ring {
		a, b := ring[i], ring[(i+1)%len(ring)]
		area += a.Lng*b.Lat - b.Lng*a.Lat
	}
	return area / 2
}

func spanMeters(ring []Point) float64 {
	bounds := BBox{South: ring[0].Lat, West: ring[0].Lng, North: ring[0].Lat, East: ring[0].Lng}
	for _, p := range ring[1:] {
		bounds.South = math.Min(bounds.South, p.Lat)
		bounds.North = math.Max(bounds.North, p.Lat)
		bounds.West = math.Min(bounds.West, p.Lng)
		bounds.East = math.Max(bounds.East, p.Lng)
	}
	northSouth := (bounds.North - bounds.South) * metersPerDegreeLat
	widestLat := math.Min(math.Abs(bounds.South), math.Abs(bounds.North))
	if bounds.South < 0 && bounds.North > 0 {
		widestLat = 0
	}
	eastWest := (bounds.East - bounds.West) * metersPerDegreeLat * math.Cos(radians(widestLat))
	return math.Max(northSouth, eastWest)
}
