package geo

import (
	"errors"
	"fmt"
	"math"
)

var (
	ErrInvalidWindow = errors.New("geo: invalid map window")
	ErrWindowTooWide = errors.New("geo: the map window spans too many tiles")
	ErrInvalidTile   = errors.New("geo: invalid map tile")
)

const (
	MaxTilesPerAxis  = 32
	MaxCellsPerAxis  = 128
	maxMercatorLat   = 85.0511287798066
	tileEdgeEpsilonD = 1e-9
)

type TileRange struct {
	Zoom       int
	MinX, MaxX int
	MinY, MaxY int
}

func (t TileRange) Key() string {
	return fmt.Sprintf("%d/%d:%d/%d:%d", t.Zoom, t.MinX, t.MaxX, t.MinY, t.MaxY)
}

type Window struct {
	BBox  BBox
	Tiles TileRange
}

func (w Window) Key() string { return w.Tiles.Key() }

func (w Window) Zoom() int { return w.Tiles.Zoom }

func (w Window) Claim() Claim {
	first := Tile{Z: w.Tiles.Zoom, X: w.Tiles.MinX, Y: w.Tiles.MinY}.Claim()
	last := Tile{Z: w.Tiles.Zoom, X: w.Tiles.MaxX, Y: w.Tiles.MaxY}.Claim()
	return Claim{West: first.West, East: last.East, South: last.South, North: first.North}
}

type Tile struct{ Z, X, Y int }

func TileAt(z, x, y int) (Tile, error) {
	if z < 0 || z > MaxZoom {
		return Tile{}, fmt.Errorf("%w: zoom %d", ErrInvalidTile, z)
	}
	n := tileCount(z)
	if x < 0 || x >= n || y < 0 || y >= n {
		return Tile{}, fmt.Errorf("%w: %d/%d/%d", ErrInvalidTile, z, x, y)
	}
	return Tile{Z: z, X: x, Y: y}, nil
}

func TileOf(p Point, z int) Tile {
	z = max(0, min(z, MaxZoom))
	return Tile{Z: z, X: tileX(p.Lng, z), Y: tileY(p.Lat, z)}
}

func (t Tile) Key() string {
	return fmt.Sprintf("%d/%d/%d", t.Z, t.X, t.Y)
}

func (t Tile) CellSize() float64 { return CellSizeDegrees(t.Z) }

func (t Tile) BBox() BBox {
	return BBox{West: tileWest(t.X, t.Z), East: tileWest(t.X+1, t.Z), North: tileNorth(t.Y, t.Z), South: tileNorth(t.Y+1, t.Z)}
}

type Claim struct{ West, East, South, North float64 }

func (t Tile) Claim() Claim {
	b := t.BBox()
	c := Claim{West: b.West, East: b.East, South: b.South, North: b.North}
	last := tileCount(t.Z) - 1
	if t.X == last {
		c.East = math.Nextafter(180, math.Inf(1))
	}
	if t.Y == 0 {
		c.North = 90
	}
	if t.Y == last {
		c.South = math.Nextafter(-90, math.Inf(-1))
	}
	return c
}

func (c Claim) Holds(p Point) bool {
	return p.Lng >= c.West && p.Lng < c.East && p.Lat > c.South && p.Lat <= c.North
}

func (t Tile) Claims(p Point) bool { return t.Claim().Holds(p) }

func (w Window) CellSize(maxPerAxis int) float64 {
	size := CellSizeDegrees(w.Zoom())
	limit := float64(max(1, maxPerAxis))
	for (w.BBox.East-w.BBox.West)/size > limit || (w.BBox.North-w.BBox.South)/size > limit {
		size *= 2
	}
	return size
}

func SnapWindow(bbox BBox, zoom float64) (Window, error) {
	if err := bbox.Validate(); err != nil {
		return Window{}, err
	}
	if !finite(zoom) || zoom < 0 || zoom >= MaxZoom+1 {
		return Window{}, fmt.Errorf("%w: zoom %v", ErrInvalidWindow, zoom)
	}
	z := int(math.Floor(zoom))
	tiles := TileRange{
		Zoom: z,
		MinX: tileX(bbox.West+tileEdgeEpsilonD, z),
		MaxX: tileX(bbox.East-tileEdgeEpsilonD, z),
		MinY: tileY(bbox.North-tileEdgeEpsilonD, z),
		MaxY: tileY(bbox.South+tileEdgeEpsilonD, z),
	}
	if tiles.MaxX-tiles.MinX+1 > MaxTilesPerAxis || tiles.MaxY-tiles.MinY+1 > MaxTilesPerAxis {
		return Window{}, fmt.Errorf("%w: %s", ErrWindowTooWide, tiles.Key())
	}
	return Window{
		Tiles: tiles,
		BBox: BBox{
			West:  tileWest(tiles.MinX, z),
			East:  tileWest(tiles.MaxX+1, z),
			North: tileNorth(tiles.MinY, z),
			South: tileNorth(tiles.MaxY+1, z),
		},
	}, nil
}

func CellIndex(p Point, cellSize float64) (int64, int64) {
	return int64(math.Floor(p.Lng / cellSize)), int64(math.Floor(p.Lat / cellSize))
}

func tileCount(z int) int { return 1 << z }

func tileX(lng float64, z int) int {
	n := tileCount(z)
	x := max(0, min(int(math.Floor((clamp(lng, -180, 180)+180)/360*float64(n))), n-1))
	if x > 0 && lng < tileWest(x, z) {
		x--
	}
	if x < n-1 && lng >= tileWest(x+1, z) {
		x++
	}
	return x
}

func tileY(lat float64, z int) int {
	n := tileCount(z)
	y := max(0, min(int(math.Floor((1-mercatorY(lat)/math.Pi)/2*float64(n))), n-1))
	if y > 0 && lat > tileNorth(y, z) {
		y--
	}
	if y < n-1 && lat <= tileNorth(y+1, z) {
		y++
	}
	return y
}

func tileWest(x, z int) float64 {
	return float64(x)/float64(tileCount(z))*360 - 180
}

func tileNorth(y, z int) float64 {
	return mercatorLat(math.Pi * (1 - 2*float64(y)/float64(tileCount(z))))
}

func mercatorY(lat float64) float64 {
	return math.Log(math.Tan(math.Pi/4 + clamp(lat, -maxMercatorLat, maxMercatorLat)*math.Pi/360))
}

func mercatorLat(y float64) float64 {
	return (2*math.Atan(math.Exp(y)) - math.Pi/2) * 180 / math.Pi
}

func clamp(v, lo, hi float64) float64 {
	return math.Min(hi, math.Max(lo, v))
}
