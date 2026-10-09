package lead

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
	"vozko/domain/leadmap"
	"vozko/infra/database"
)

func tileLayer(t *testing.T, reader leadmap.Reader, ws string, q leadmap.LayerRequest) leadmap.Layer {
	t.Helper()
	layer, err := reader.Layer(context.Background(), leadmap.Scope{WorkspaceID: ws}, q)
	if err != nil {
		t.Fatal(err)
	}
	return layer
}

func peopleIn(l leadmap.Layer) int {
	people := 0
	for _, p := range l.Points {
		people += p.People
	}
	for _, c := range l.Cells {
		people += c.People
	}
	return people
}

func TestTilesShareTheLeadsSoALeadOnATileEdgeIsCountedOnceAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	ws := uuid.NewString()
	z := 14
	home, err := geo.TileAt(z, 6067, 9295)
	if err != nil {
		t.Fatal(err)
	}
	b := home.BBox()
	midLat, midLng := (b.South+b.North)/2, (b.West+b.East)/2
	positions := []geo.Point{
		{Lat: midLat, Lng: midLng},
		{Lat: midLat, Lng: b.West},
		{Lat: midLat, Lng: b.East},
		{Lat: b.North, Lng: midLng},
		{Lat: b.South, Lng: midLng},
		{Lat: b.North, Lng: b.West},
	}
	for _, p := range positions {
		lat, lng := at(p.Lat, p.Lng)
		seedLead(t, db, ws, &seededPosition{lat: lat, lng: lng, precision: "exact"})
	}
	reader := NewMapReader(db)
	for _, asCells := range []bool{false, true} {
		total := 0
		for x := home.X - 1; x <= home.X+1; x++ {
			for y := home.Y - 1; y <= home.Y+1; y++ {
				tile, err := geo.TileAt(z, x, y)
				if err != nil {
					t.Fatal(err)
				}
				q := leadmap.NewTileRequest(tile, nil)
				if asCells {
					q.MaxPoints = 0
				}
				layer := tileLayer(t, reader, ws, q)
				for _, p := range layer.Points {
					if !tile.Claims(p.Position) {
						t.Fatalf("tile %s drew %+v outside its claim", tile.Key(), p.Position)
					}
				}
				total += peopleIn(layer)
			}
		}
		if total != len(positions) {
			t.Fatalf("cells=%v: the nine tiles hold %d people, want each of the %d exactly once", asCells, total, len(positions))
		}
	}
	for _, p := range positions {
		owner := geo.TileOf(p, z)
		if peopleIn(tileLayer(t, reader, ws, leadmap.NewTileRequest(owner, nil))) == 0 {
			t.Fatalf("the tile %s that claims %+v drew nobody", owner.Key(), p)
		}
	}
}

func TestADenseTileAnswersCellsThatAddUpToItsPeopleAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	ws := uuid.NewString()
	tile := geo.TileOf(geo.Point{Lat: -23.565, Lng: -46.655}, 12)
	top := tile.BBox().North - 0.0001
	err := db.Exec(`WITH made AS (
			INSERT INTO leads (id, workspace_id, name, created_at, updated_at)
			SELECT gen_random_uuid(), ?, 'Lead ' || i, now(), now() FROM generate_series(1, ?) i RETURNING id
		)
		INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, position, latitude, longitude, geo_precision, geo_status, fingerprint, created_at, updated_at)
		SELECT gen_random_uuid(), ?, id, 'home', true, 0, ?::float8 - (row_number() OVER ()) * 0.00001, -46.655, 'street', 'located', left(md5(id::text), 32), now(), now() FROM made`,
		ws, leadmap.MaxTilePoints+1, ws, top).Error
	if err != nil {
		t.Fatal(err)
	}
	reader := NewMapReader(db)
	cells := tileLayer(t, reader, ws, leadmap.NewTileRequest(tile, nil))
	if cells.Kind != leadmap.LayerCells || cells.CellSize != tile.CellSize() || peopleIn(cells) != leadmap.MaxTilePoints+1 {
		t.Fatalf("1,001 positions in one tile = %s of %v holding %d, want cells of the tile grid holding everyone", cells.Kind, cells.CellSize, peopleIn(cells))
	}
	if err := db.Exec(`UPDATE leads SET deleted_at = now(), version = version + 1 WHERE id = (SELECT id FROM leads WHERE workspace_id = ? LIMIT 1)`, ws).Error; err != nil {
		t.Fatal(err)
	}
	points := tileLayer(t, reader, ws, leadmap.NewTileRequest(tile, nil))
	if points.Kind != leadmap.LayerPoints || len(points.Points) != leadmap.MaxTilePoints {
		t.Fatalf("1,000 positions = %s with %d points, want every position as a point", points.Kind, len(points.Points))
	}
}

const tileWarmBudgetMS = 150.0

func TestTilesStayInsideTheirBudgetOnAWorkspaceOf188kLeadsAgainstPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("generates 188k leads")
	}
	db := mapDB(t)
	volumeTables(t, db)
	volumeIndexes(t, db)
	ws, noise := uuid.NewString(), uuid.NewString()
	seedWorkspace(t, db, ws, 188000)
	seedWorkspace(t, db, noise, 60000)
	live, ok := database.ConcurrentIndexSQL(database.LeadLiveIndex)
	if !ok {
		t.Fatalf("index %s is not declared", database.LeadLiveIndex)
	}
	if err := db.Exec(live).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"leads", "lead_addresses"} {
		if err := db.Exec("VACUUM ANALYZE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	reader := &mapReader{db: db, leads: &repository{db: db, agg: newAggregateCache(nil)}}
	everyone, err := reader.scope(leadmap.Scope{WorkspaceID: ws, Filter: crmfilter.Filter{}})
	if err != nil {
		t.Fatal(err)
	}
	centre := geo.Point{Lat: -23.55, Lng: -46.63}
	coloured := func(q leadmap.LayerRequest) leadmap.LayerRequest {
		q.ColorByKey = "classificacao"
		return q
	}
	probes := []struct {
		name string
		tile geo.Tile
		q    func(geo.Tile) leadmap.LayerRequest
		kind leadmap.LayerKind
	}{
		{"national tile z4", geo.TileOf(centre, 4), func(t geo.Tile) leadmap.LayerRequest { return leadmap.NewTileRequest(t, nil) }, leadmap.LayerCells},
		{"national tile z4 coloured", geo.TileOf(centre, 4), func(t geo.Tile) leadmap.LayerRequest { return coloured(leadmap.NewTileRequest(t, nil)) }, leadmap.LayerCells},
		{"dense city tile z12", geo.TileOf(centre, 12), func(t geo.Tile) leadmap.LayerRequest { return leadmap.NewTileRequest(t, nil) }, leadmap.LayerCells},
		{"dense city tile z12 coloured", geo.TileOf(centre, 12), func(t geo.Tile) leadmap.LayerRequest { return coloured(leadmap.NewTileRequest(t, nil)) }, leadmap.LayerCells},
		{"street tile z15", geo.TileOf(centre, 15), func(t geo.Tile) leadmap.LayerRequest { return leadmap.NewTileRequest(t, nil) }, leadmap.LayerPoints},
		{"street tile z15 coloured", geo.TileOf(centre, 15), func(t geo.Tile) leadmap.LayerRequest { return coloured(leadmap.NewTileRequest(t, nil)) }, leadmap.LayerPoints},
	}
	var over []string
	for _, p := range probes {
		q := p.q(p.tile)
		layer := tileLayer(t, reader, ws, q)
		if layer.Kind != p.kind || peopleIn(layer) == 0 {
			t.Fatalf("%s answered %s with %d people, want %s with people", p.name, layer.Kind, peopleIn(layer), p.kind)
		}
		measure := func() float64 {
			sql, args := everyone.layerPositions(q)
			ms := explainMS(t, db, everyone, sql, args)
			if layer.Kind == leadmap.LayerPoints {
				sql, args = everyone.layerPoints(q)
			} else {
				sql, args = everyone.layerCells(q)
			}
			return ms + explainMS(t, db, everyone, sql, args)
		}
		first := measure()
		warm := math.Inf(1)
		for i := 0; i < 5; i++ {
			warm = math.Min(warm, measure())
		}
		t.Logf("%-32s %s %-6s %6d people, first %7.1f ms, best warm %7.1f ms", p.name, p.tile.Key(), layer.Kind, peopleIn(layer), first, warm)
		if warm > tileWarmBudgetMS {
			over = append(over, p.name)
			sql, args := everyone.layerCells(q)
			t.Logf("plan of %s:\n%s", p.name, explainPlan(t, db, everyone, sql, args))
		}
	}
	logPanAcrossOneTileEdge(t, reader, ws)
	if len(over) > 0 {
		t.Fatalf("over the %.0f ms warm budget: %s", tileWarmBudgetMS, strings.Join(over, "; "))
	}
}

func logPanAcrossOneTileEdge(t *testing.T, reader leadmap.Reader, ws string) {
	t.Helper()
	z := 12
	centre := geo.TileOf(geo.Point{Lat: -23.55, Lng: -46.63}, z)
	window := func(minX int) geo.Window {
		west, _ := geo.TileAt(z, minX, centre.Y-1)
		east, _ := geo.TileAt(z, minX+2, centre.Y)
		w, err := geo.SnapWindow(geo.BBox{West: west.BBox().West + 1e-6, North: west.BBox().North - 1e-6, East: east.BBox().East - 1e-6, South: east.BBox().South + 1e-6}, float64(z))
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	read := func(q leadmap.LayerRequest) float64 {
		return wallMS(t, func() error {
			_, err := reader.Layer(context.Background(), leadmap.Scope{WorkspaceID: ws}, q)
			return err
		})
	}
	best := func(q leadmap.LayerRequest) float64 {
		ms := math.Inf(1)
		for i := 0; i < 3; i++ {
			ms = math.Min(ms, read(q))
		}
		return ms
	}
	moved := window(centre.X)
	before := best(leadmap.NewLayerRequest(moved, nil))
	var after, slowest float64
	requests := 0
	for y := centre.Y - 1; y <= centre.Y; y++ {
		tile, err := geo.TileAt(z, centre.X+2, y)
		if err != nil {
			t.Fatal(err)
		}
		ms := best(leadmap.NewTileRequest(tile, nil))
		after += ms
		slowest = math.Max(slowest, ms)
		requests++
	}
	t.Logf("pan across one tile edge at z%d (window %s, 3x2 tiles): before 1 request for the whole window, %.1f ms; after %d requests for the new column only, %.1f ms in all, slowest %.1f ms, 4 tiles reused", z, moved.Key(), before, requests, after, slowest)
}
