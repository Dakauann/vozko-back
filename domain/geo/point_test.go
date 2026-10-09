package geo

import (
	"errors"
	"math"
	"testing"
)

func TestPointValidate(t *testing.T) {
	tests := []struct {
		name  string
		point Point
		valid bool
	}{
		{"accepts Avenida Paulista", Point{Lat: -23.5614, Lng: -46.6559}, true},
		{"accepts the poles and the antimeridian", Point{Lat: 90, Lng: 180}, true},
		{"refuses null island, the default of a missing fix", Point{}, false},
		{"refuses a latitude above 90", Point{Lat: 90.0001, Lng: -46}, false},
		{"refuses a latitude below -90", Point{Lat: -90.0001, Lng: -46}, false},
		{"refuses a longitude above 180", Point{Lat: -23, Lng: 180.0001}, false},
		{"refuses a longitude below -180", Point{Lat: -23, Lng: -180.0001}, false},
		{"refuses NaN", Point{Lat: math.NaN(), Lng: -46}, false},
		{"refuses infinity", Point{Lat: -23, Lng: math.Inf(-1)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.point.Validate()
			if tt.valid && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !tt.valid && !errors.Is(err, ErrInvalidPoint) {
				t.Fatalf("expected ErrInvalidPoint, got %v", err)
			}
		})
	}
}

func TestPointInBrazil(t *testing.T) {
	tests := []struct {
		name   string
		point  Point
		inside bool
	}{
		{"São Paulo", Point{Lat: -23.5505, Lng: -46.6333}, true},
		{"Chuí, the southern tip", Point{Lat: -33.6866, Lng: -53.4594}, true},
		{"Monte Caburaí, the northern tip", Point{Lat: 5.2717, Lng: -60.2128}, true},
		{"Fernando de Noronha", Point{Lat: -3.8576, Lng: -32.4297}, true},
		{"Mâncio Lima, the western tip", Point{Lat: -7.6142, Lng: -72.8958}, true},
		{"Lisbon", Point{Lat: 38.7223, Lng: -9.1393}, false},
		{"Buenos Aires latitude far south of Chuí", Point{Lat: -34.6037, Lng: -58.3816}, false},
		{"an invalid point is never in Brazil", Point{Lat: math.NaN(), Lng: -46}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.point.InBrazil(); got != tt.inside {
				t.Fatalf("InBrazil() = %v, expected %v", got, tt.inside)
			}
		})
	}
}

func TestDistanceMeters(t *testing.T) {
	saoPaulo := Point{Lat: -23.5505, Lng: -46.6333}
	rio := Point{Lat: -22.9068, Lng: -43.1729}
	tests := []struct {
		name      string
		a, b      Point
		expected  float64
		tolerance float64
	}{
		{"the same point is zero", saoPaulo, saoPaulo, 0, 0.001},
		{"one degree of latitude is about 111 km", Point{Lat: -10, Lng: -50}, Point{Lat: -11, Lng: -50}, 111195, 5},
		{"São Paulo to Rio de Janeiro is about 361 km", saoPaulo, rio, 360750, 1500},
		{"distance is symmetric", rio, saoPaulo, 360750, 1500},
		{"one degree of longitude shrinks with cos(lat)", Point{Lat: -60, Lng: -50}, Point{Lat: -60, Lng: -51}, 55597, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DistanceMeters(tt.a, tt.b)
			if math.Abs(got-tt.expected) > tt.tolerance {
				t.Fatalf("DistanceMeters = %.1f, expected %.1f within %.1f", got, tt.expected, tt.tolerance)
			}
		})
	}
}

func TestBBoxValidate(t *testing.T) {
	tests := []struct {
		name  string
		box   BBox
		valid bool
	}{
		{"accepts a São Paulo viewport", BBox{South: -23.7, West: -46.8, North: -23.4, East: -46.4}, true},
		{"refuses south above north", BBox{South: -23.4, West: -46.8, North: -23.7, East: -46.4}, false},
		{"refuses west above east", BBox{South: -23.7, West: -46.4, North: -23.4, East: -46.8}, false},
		{"refuses an empty box", BBox{South: -23.5, West: -46.5, North: -23.5, East: -46.5}, false},
		{"refuses an out of range latitude", BBox{South: -91, West: -46.8, North: -23.4, East: -46.4}, false},
		{"refuses NaN", BBox{South: math.NaN(), West: -46.8, North: -23.4, East: -46.4}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.box.Validate()
			if tt.valid && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !tt.valid && !errors.Is(err, ErrInvalidBBox) {
				t.Fatalf("expected ErrInvalidBBox, got %v", err)
			}
		})
	}
}

func TestCellSizeDegrees(t *testing.T) {
	tests := []struct {
		name     string
		zoom     int
		expected float64
	}{
		{"the whole world at zoom 0", 0, 11.25},
		{"halves with each zoom level", 1, 5.625},
		{"a city at zoom 10", 10, 360.0 / 1024 / 32},
		{"a street at zoom 16", 16, 360.0 / 65536 / 32},
		{"a negative zoom is clamped to 0", -3, 11.25},
		{"a zoom past the deepest level is clamped", 40, 360.0 / float64(int64(1)<<MaxZoom) / 32},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CellSizeDegrees(tt.zoom); math.Abs(got-tt.expected) > 1e-12 {
				t.Fatalf("CellSizeDegrees(%d) = %v, expected %v", tt.zoom, got, tt.expected)
			}
		})
	}
}
