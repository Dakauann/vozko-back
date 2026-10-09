package geo

import (
	"errors"
	"math"
)

var (
	ErrInvalidPoint = errors.New("geo: invalid point")
	ErrInvalidBBox  = errors.New("geo: invalid bounding box")
)

const (
	earthRadiusM       = 6371008.8
	metersPerDegreeLat = earthRadiusM * math.Pi / 180
	MaxZoom            = 22
	cellsPerTile       = 32
)

var brazilBounds = BBox{South: -33.8, West: -74.1, North: 5.3, East: -28.8}

type Point struct{ Lat, Lng float64 }

func (p Point) Validate() error {
	if !finite(p.Lat) || !finite(p.Lng) {
		return ErrInvalidPoint
	}
	if p.Lat < -90 || p.Lat > 90 || p.Lng < -180 || p.Lng > 180 {
		return ErrInvalidPoint
	}
	if p.Lat == 0 && p.Lng == 0 {
		return ErrInvalidPoint
	}
	return nil
}

func (p Point) InBrazil() bool {
	return p.Validate() == nil && brazilBounds.contains(p)
}

func DistanceMeters(a, b Point) float64 {
	lat1, lat2 := radians(a.Lat), radians(b.Lat)
	dLat := lat2 - lat1
	dLng := radians(b.Lng - a.Lng)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}

type BBox struct{ South, West, North, East float64 }

func (b BBox) Validate() error {
	for _, v := range []float64{b.South, b.West, b.North, b.East} {
		if !finite(v) {
			return ErrInvalidBBox
		}
	}
	if b.South < -90 || b.North > 90 || b.West < -180 || b.East > 180 {
		return ErrInvalidBBox
	}
	if b.South >= b.North || b.West >= b.East {
		return ErrInvalidBBox
	}
	return nil
}

func BrazilBounds() BBox { return brazilBounds }

func (b BBox) contains(p Point) bool {
	return p.Lat >= b.South && p.Lat <= b.North && p.Lng >= b.West && p.Lng <= b.East
}

func CellSizeDegrees(zoom int) float64 {
	zoom = max(0, min(zoom, MaxZoom))
	return 360 / float64(int64(1)<<zoom) / cellsPerTile
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func radians(degrees float64) float64 {
	return degrees * math.Pi / 180
}
