package geo

import (
	"errors"
	"math"
	"testing"
)

func TestSnapWindowCoversTheViewportWithWholeTiles(t *testing.T) {
	paulista := BBox{South: -23.5700, West: -46.6700, North: -23.5500, East: -46.6400}
	window, err := SnapWindow(paulista, 14.7)
	if err != nil {
		t.Fatal(err)
	}
	if window.Tiles.Zoom != 14 {
		t.Fatalf("zoom = %d, want the floor of 14.7", window.Tiles.Zoom)
	}
	want := TileRange{Zoom: 14, MinX: 6067, MaxX: 6069, MinY: 9295, MaxY: 9296}
	if window.Tiles != want {
		t.Fatalf("tiles = %+v, want %+v", window.Tiles, want)
	}
	if window.Key() != "14/6067:6069/9295:9296" {
		t.Fatalf("key = %q, want the front's tile key format", window.Key())
	}
	b := window.BBox
	if b.West > paulista.West || b.East < paulista.East || b.South > paulista.South || b.North < paulista.North {
		t.Fatalf("snapped bbox %+v must contain the viewport %+v", b, paulista)
	}
}

func TestSnapWindowIsIdempotentSoTheFrontSnappedBoxKeepsItsKey(t *testing.T) {
	cases := []struct {
		name string
		bbox BBox
		zoom float64
	}{
		{"a São Paulo street", BBox{South: -23.5700, West: -46.6700, North: -23.5500, East: -46.6400}, 15.2},
		{"a whole city", BBox{South: -19.98, West: -44.06, North: -19.78, East: -43.86}, 11},
		{"the country", BBox{South: -33.8, West: -74.1, North: 5.3, East: -28.8}, 4},
		{"the world at zoom zero", BBox{South: -85, West: -180, North: 85, East: 180}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first, err := SnapWindow(tc.bbox, tc.zoom)
			if err != nil {
				t.Fatal(err)
			}
			second, err := SnapWindow(first.BBox, float64(first.Tiles.Zoom))
			if err != nil {
				t.Fatal(err)
			}
			if first.Tiles != second.Tiles {
				t.Fatalf("re-snapping moved the tiles from %+v to %+v", first.Tiles, second.Tiles)
			}
		})
	}
}

func TestSnapWindowRefusesWhatCannotBeAViewport(t *testing.T) {
	cases := []struct {
		name string
		bbox BBox
		zoom float64
		want error
	}{
		{"an inverted box", BBox{South: -23.5, West: -46.6, North: -23.6, East: -46.5}, 12, ErrInvalidBBox},
		{"NaN in the box", BBox{South: math.NaN(), West: -46.6, North: -23.5, East: -46.5}, 12, ErrInvalidBBox},
		{"a NaN zoom", BBox{South: -23.6, West: -46.6, North: -23.5, East: -46.5}, math.NaN(), ErrInvalidWindow},
		{"a negative zoom", BBox{South: -23.6, West: -46.6, North: -23.5, East: -46.5}, -1, ErrInvalidWindow},
		{"a zoom past the deepest tile", BBox{South: -23.6, West: -46.6, North: -23.5, East: -46.5}, 23, ErrInvalidWindow},
		{"the whole country at street zoom", BBox{South: -33.8, West: -74.1, North: 5.3, East: -28.8}, 16, ErrWindowTooWide},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := SnapWindow(tc.bbox, tc.zoom); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCellIndexMatchesTheCellSizeOfTheZoom(t *testing.T) {
	size := CellSizeDegrees(12)
	p := Point{Lat: -23.5614, Lng: -46.6559}
	ix, iy := CellIndex(p, size)
	if ix != int64(math.Floor(p.Lng/size)) || iy != int64(math.Floor(p.Lat/size)) {
		t.Fatalf("cell = (%d, %d), want the floor of the position over the cell size", ix, iy)
	}
	if ix >= 0 || iy >= 0 {
		t.Fatalf("a point west and south of zero lives in negative cells, got (%d, %d)", ix, iy)
	}
}

func TestPinningPrecisionsAreExactlyThoseThatPinAHouseBestFirst(t *testing.T) {
	got := PinningPrecisions()
	want := []Precision{PrecisionExact, PrecisionAddress, PrecisionStreet}
	if len(got) != len(want) {
		t.Fatalf("pinning precisions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] || !got[i].PinsAHouse() {
			t.Fatalf("pinning precisions = %v, want %v", got, want)
		}
	}
	all := PrecisionsBestFirst()
	if len(all) != 6 || all[0] != PrecisionExact || all[5] != PrecisionCity {
		t.Fatalf("precisions best first = %v", all)
	}
	for i := 1; i < len(all); i++ {
		if !all[i-1].Better(all[i]) {
			t.Fatalf("%s must be better than %s", all[i-1], all[i])
		}
	}
}

func TestBrazilBoundsHoldTheCountryAndValidate(t *testing.T) {
	b := BrazilBounds()
	if b.Validate() != nil || !(Point{Lat: -23.5505, Lng: -46.6333}).InBrazil() || (Point{Lat: 38.7223, Lng: -9.1393}).InBrazil() {
		t.Fatalf("Brazil bounds = %+v", b)
	}
}

func TestWindowCellSizeKeepsTheCellsOfOneAxisUnderTheBound(t *testing.T) {
	cases := []struct {
		name string
		bbox BBox
		zoom float64
	}{
		{"a street window", BBox{South: -23.5700, West: -46.6700, North: -23.5500, East: -46.6400}, 15},
		{"thirty two tiles at street zoom", BBox{South: -23.70, West: -46.80, North: -23.50, East: -46.40}, 14},
		{"a whole city", BBox{South: -23.80, West: -46.88, North: -23.30, East: -46.38}, 10},
		{"the country", BBox{South: -33.8, West: -74.1, North: 5.3, East: -28.8}, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := SnapWindow(tc.bbox, tc.zoom)
			if err != nil {
				t.Fatal(err)
			}
			size := w.CellSize(MaxCellsPerAxis)
			base := CellSizeDegrees(w.Zoom())
			if size < base {
				t.Fatalf("cell size %v is finer than the zoom's own %v", size, base)
			}
			ratio := size / base
			if ratio != math.Exp2(math.Round(math.Log2(ratio))) {
				t.Fatalf("cell size %v is not the zoom's size times a power of two", size)
			}
			minX, _ := CellIndex(Point{Lat: w.BBox.South, Lng: w.BBox.West}, size)
			maxX, _ := CellIndex(Point{Lat: w.BBox.South, Lng: w.BBox.East}, size)
			_, minY := CellIndex(Point{Lat: w.BBox.South, Lng: w.BBox.West}, size)
			_, maxY := CellIndex(Point{Lat: w.BBox.North, Lng: w.BBox.West}, size)
			if maxX-minX+1 > MaxCellsPerAxis+1 || maxY-minY+1 > MaxCellsPerAxis+1 {
				t.Fatalf("window holds %d x %d cells, want at most %d per axis", maxX-minX+1, maxY-minY+1, MaxCellsPerAxis+1)
			}
			if size > base && (w.BBox.East-w.BBox.West)/(size/2) <= MaxCellsPerAxis && (w.BBox.North-w.BBox.South)/(size/2) <= MaxCellsPerAxis {
				t.Fatalf("cell size %v is coarser than the bound needs", size)
			}
		})
	}
}

func TestEveryKnownPrecisionIsRankedBestFirst(t *testing.T) {
	all := PrecisionsBestFirst()
	for _, p := range []Precision{PrecisionExact, PrecisionAddress, PrecisionStreet, PrecisionPostalCode, PrecisionDistrict, PrecisionCity} {
		if !p.Known() {
			t.Fatalf("%s must be known", p)
		}
		found := false
		for _, ranked := range all {
			found = found || ranked == p
		}
		if !found {
			t.Fatalf("%s is missing from %v", p, all)
		}
	}
	all[0] = PrecisionCity
	if PrecisionsBestFirst()[0] != PrecisionExact {
		t.Fatal("the ranking must not be changed through a returned slice")
	}
}

func TestATileOutsideTheZoomsOrTheGridIsRefused(t *testing.T) {
	cases := []struct {
		name    string
		z, x, y int
	}{
		{"a negative zoom", -1, 0, 0},
		{"a zoom past the deepest tile", MaxZoom + 1, 0, 0},
		{"a negative column", 4, -1, 3},
		{"a column past the grid", 4, 16, 3},
		{"a negative row", 4, 3, -1},
		{"a row past the grid", 4, 3, 16},
		{"a column past the grid at zoom zero", 0, 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := TileAt(tc.z, tc.x, tc.y); !errors.Is(err, ErrInvalidTile) {
				t.Fatalf("TileAt(%d, %d, %d) = %v, want ErrInvalidTile", tc.z, tc.x, tc.y, err)
			}
		})
	}
}

func TestATileHasTheKeyAndTheThirtyTwoCellsOfItsZoom(t *testing.T) {
	tile, err := TileAt(14, 6067, 9295)
	if err != nil {
		t.Fatal(err)
	}
	if tile.Key() != "14/6067/9295" {
		t.Fatalf("key = %q, want z/x/y", tile.Key())
	}
	if tile.CellSize() != CellSizeDegrees(14) {
		t.Fatalf("cell size = %v, want the zoom's %v", tile.CellSize(), CellSizeDegrees(14))
	}
	b := tile.BBox()
	if got := (b.East - b.West) / tile.CellSize(); math.Abs(got-32) > 1e-9 {
		t.Fatalf("a tile is %v cells wide, want 32", got)
	}
}

func TestATileHasTheBoxTheWindowOfThatOneTileSnapsTo(t *testing.T) {
	tile, err := TileAt(14, 6067, 9295)
	if err != nil {
		t.Fatal(err)
	}
	window, err := SnapWindow(BBox{South: -23.5700, West: -46.6700, North: -23.5500, East: -46.6400}, 14)
	if err != nil {
		t.Fatal(err)
	}
	b := tile.BBox()
	if b.West != window.BBox.West || b.North != window.BBox.North {
		t.Fatalf("tile box %+v must start where the window of its range starts %+v", b, window.BBox)
	}
	if b.Validate() != nil {
		t.Fatalf("tile box %+v must validate", b)
	}
}

func TestTileOfFindsTheOneTileThatClaimsAPoint(t *testing.T) {
	points := []Point{
		{Lat: -23.5614, Lng: -46.6559},
		{Lat: -19.92, Lng: -43.94},
		{Lat: -3.1190, Lng: -60.0217},
		{Lat: 2.82, Lng: -60.67},
	}
	for _, p := range points {
		for z := 0; z <= MaxZoom; z++ {
			tile := TileOf(p, z)
			if tile.Z != z || !tile.Claims(p) {
				t.Fatalf("TileOf(%+v, %d) = %+v does not claim the point", p, z, tile)
			}
			for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				next, err := TileAt(z, tile.X+d[0], tile.Y+d[1])
				if err == nil && next.Claims(p) {
					t.Fatalf("neighbour %+v also claims %+v", next, p)
				}
			}
			window, err := SnapWindow(BBox{South: p.Lat - 1e-7, West: p.Lng - 1e-7, North: p.Lat + 1e-7, East: p.Lng + 1e-7}, float64(z))
			if err == nil && (window.Tiles.MinX > tile.X || window.Tiles.MaxX < tile.X || window.Tiles.MinY > tile.Y || window.Tiles.MaxY < tile.Y) {
				t.Fatalf("the window around %+v at %d covers %+v, not the tile %+v", p, z, window.Tiles, tile)
			}
		}
	}
}

func TestAPointOnASharedEdgeBelongsToExactlyOneTile(t *testing.T) {
	z := 10
	tile, err := TileAt(z, 378, 580)
	if err != nil {
		t.Fatal(err)
	}
	b := tile.BBox()
	edges := []Point{
		{Lat: (b.South + b.North) / 2, Lng: b.West},
		{Lat: (b.South + b.North) / 2, Lng: b.East},
		{Lat: b.North, Lng: (b.West + b.East) / 2},
		{Lat: b.South, Lng: (b.West + b.East) / 2},
		{Lat: b.North, Lng: b.West},
	}
	for _, p := range edges {
		claimed := 0
		for x := tile.X - 1; x <= tile.X+1; x++ {
			for y := tile.Y - 1; y <= tile.Y+1; y++ {
				if next, err := TileAt(z, x, y); err == nil && next.Claims(p) {
					claimed++
				}
			}
		}
		if claimed != 1 {
			t.Fatalf("the edge point %+v is claimed by %d tiles, want exactly one", p, claimed)
		}
		if !TileOf(p, z).Claims(p) {
			t.Fatalf("TileOf(%+v) does not claim its own edge point", p)
		}
	}
}

func TestTheTilesOnTheRimOfTheGridClaimTheRestOfTheWorld(t *testing.T) {
	z := 3
	last := tileCount(z) - 1
	rim := []struct {
		p    Point
		x, y int
	}{
		{Point{Lat: 10, Lng: 180}, last, 3},
		{Point{Lat: 89.9, Lng: 10}, 4, 0},
		{Point{Lat: 90, Lng: 10}, 4, 0},
		{Point{Lat: -89.9, Lng: 10}, 4, last},
		{Point{Lat: -90, Lng: -180}, 0, last},
	}
	for _, r := range rim {
		tile, err := TileAt(z, r.x, r.y)
		if err != nil {
			t.Fatal(err)
		}
		if !tile.Claims(r.p) || TileOf(r.p, z) != tile {
			t.Fatalf("%+v must belong to the rim tile %+v, TileOf = %+v", r.p, tile, TileOf(r.p, z))
		}
	}
}
