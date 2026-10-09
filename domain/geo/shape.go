package geo

import (
	"errors"
	"fmt"
	"math"
)

var (
	ErrUnknownShapeKind   = errors.New("geo: unknown shape kind")
	ErrInvalidShape       = errors.New("geo: invalid shape")
	ErrTooManyVertices    = errors.New("geo: too many vertices")
	ErrSelfCrossing       = errors.New("geo: ring crosses itself")
	ErrEmptyShape         = errors.New("geo: the shape encloses no area")
	ErrShapeOutsideBounds = errors.New("geo: the shape leaves the map of Brazil")
	ErrInvalidRadius      = errors.New("geo: invalid radius")
)

const (
	MaxRingVertices          = 200
	CircleVertices           = 64
	RectangleCornerTolerance = 1e-3
	minRingArea              = 1e-12
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
	_, err := s.Normalized()
	return err
}

func (s Shape) Normalized() (Shape, error) {
	switch s.Kind {
	case ShapePolygon:
		return s, s.validateRing()
	case ShapeRectangle:
		if err := s.validateRing(); err != nil {
			return Shape{}, err
		}
		corners, ok := boxCorners(openRing(s.Ring))
		if !ok {
			return Shape{}, fmt.Errorf("%w: a rectangle needs four axis-aligned corners", ErrInvalidShape)
		}
		return Shape{Kind: ShapeRectangle, Ring: corners}, nil
	case ShapeCircle:
		return s, s.validateCircle()
	}
	return Shape{}, ErrUnknownShapeKind
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
	normalized, err := s.Normalized()
	if err != nil {
		return nil
	}
	if normalized.Kind == ShapeCircle {
		return circleRing(normalized.Center, normalized.RadiusM)
	}
	return append([]Point(nil), openRing(normalized.Ring)...)
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
		if !shapeBounds.contains(vertex) {
			return fmt.Errorf("%w: vertex %d", ErrShapeOutsideBounds, i)
		}
	}
	if collinear(ring) {
		return ErrEmptyShape
	}
	if selfCrossing(ring) {
		return ErrSelfCrossing
	}
	if math.Abs(signedArea(ring)) < minRingArea {
		return ErrEmptyShape
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
	if !finite(s.RadiusM) || s.RadiusM < 0 {
		return ErrInvalidRadius
	}
	if s.RadiusM == 0 {
		return ErrEmptyShape
	}
	if !circleWithin(s.Center, s.RadiusM, shapeBounds) {
		return ErrShapeOutsideBounds
	}
	return nil
}

func circleWithin(center Point, radiusM float64, bounds BBox) bool {
	reach := radiusM / metersPerDegreeLat
	if !bounds.contains(Point{Lat: center.Lat - reach, Lng: center.Lng}) || !bounds.contains(Point{Lat: center.Lat + reach, Lng: center.Lng}) {
		return false
	}
	for _, vertex := range circleRing(center, radiusM) {
		if !finite(vertex.Lat) || !finite(vertex.Lng) || !bounds.contains(vertex) {
			return false
		}
	}
	return true
}

func openRing(ring []Point) []Point {
	if len(ring) > 1 && ring[0] == ring[len(ring)-1] {
		return ring[:len(ring)-1]
	}
	return ring
}

func boxCorners(ring []Point) ([]Point, bool) {
	if len(ring) != 4 {
		return nil, false
	}
	box := boundsOf(ring)
	tolerance := RectangleCornerTolerance * math.Min(box.North-box.South, box.East-box.West)
	corners := make([]Point, len(ring))
	seen := map[Point]bool{}
	for i, corner := range ring {
		lat, latOK := snapTo(corner.Lat, box.South, box.North, tolerance)
		lng, lngOK := snapTo(corner.Lng, box.West, box.East, tolerance)
		corners[i] = Point{Lat: lat, Lng: lng}
		if !latOK || !lngOK || seen[corners[i]] {
			return nil, false
		}
		seen[corners[i]] = true
	}
	for i := range corners {
		a, b := corners[i], corners[(i+1)%len(corners)]
		if (a.Lat == b.Lat) == (a.Lng == b.Lng) {
			return nil, false
		}
	}
	return corners, true
}

func snapTo(v, low, high, tolerance float64) (float64, bool) {
	toLow, toHigh := math.Abs(v-low), math.Abs(v-high)
	switch {
	case toLow <= toHigh && toLow <= tolerance:
		return low, true
	case toHigh <= tolerance:
		return high, true
	}
	return v, false
}

func boundsOf(ring []Point) BBox {
	box := BBox{South: ring[0].Lat, West: ring[0].Lng, North: ring[0].Lat, East: ring[0].Lng}
	for _, p := range ring[1:] {
		box.South = math.Min(box.South, p.Lat)
		box.North = math.Max(box.North, p.Lat)
		box.West = math.Min(box.West, p.Lng)
		box.East = math.Max(box.East, p.Lng)
	}
	return box
}

func collinear(ring []Point) bool {
	origin := ring[0]
	for _, toward := range ring[1:] {
		if toward == origin {
			continue
		}
		for _, p := range ring {
			if math.Abs(orientation(origin, toward, p)) > minRingArea {
				return false
			}
		}
		return true
	}
	return true
}

func circleRing(center Point, radiusM float64) []Point {
	ring := make([]Point, 0, CircleVertices)
	fromLat := radians(center.Lat)
	reach := radiusM / earthRadiusM
	for i := 0; i < CircleVertices; i++ {
		bearing := 2 * math.Pi * float64(i) / CircleVertices
		toLat := math.Asin(math.Sin(fromLat)*math.Cos(reach) + math.Cos(fromLat)*math.Sin(reach)*math.Cos(bearing))
		turn := math.Atan2(math.Sin(bearing)*math.Sin(reach)*math.Cos(fromLat), math.Cos(reach)-math.Sin(fromLat)*math.Sin(toLat))
		ring = append(ring, Point{Lat: degrees(toLat), Lng: center.Lng + degrees(turn)})
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
