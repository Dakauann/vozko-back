package lead

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/domain/leadarea"
	"vozko/domain/leadmap"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	leadarea_repository "vozko/infra/repositories/leadarea"
	"vozko/infra/repositories/repotest"
)

func mapDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "lead_map")
	migrateLeadTables(t, db)
	if err := db.AutoMigrate(&schema.LeadArea{}, &schema.GeoCity{}, &schema.GeoDistrictPoint{}); err != nil {
		t.Fatalf("migrate areas and reference points: %v", err)
	}
	for _, name := range []string{database.LeadAddressWindowIndex, "idx_lead_addresses_workspace_place"} {
		sql, ok := database.ConcurrentIndexSQL(name)
		if !ok {
			t.Fatalf("index %s is not declared", name)
		}
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("index %s: %v", name, err)
		}
	}
	return db
}

type seededPosition struct {
	lat, lng           *float64
	precision, status  string
	city, district     string
	cityKey, districtK string
	cityCode           string
}

func at(lat, lng float64) (*float64, *float64) { return &lat, &lng }

func layerIn(t *testing.T, db *gorm.DB, ws string, f crmfilter.Filter, w geo.Window) leadmap.Layer {
	t.Helper()
	layer, err := NewMapReader(db).Layer(context.Background(), leadmap.Scope{WorkspaceID: ws, Filter: f}, leadmap.NewLayerRequest(w, nil))
	if err != nil {
		t.Fatal(err)
	}
	return layer
}

func seedLead(t *testing.T, db *gorm.DB, ws string, p *seededPosition) string {
	t.Helper()
	id := uuid.NewString()
	now := time.Now()
	if err := db.Exec(`INSERT INTO leads (id, workspace_id, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, id, ws, "Lead "+id[:6], now, now).Error; err != nil {
		t.Fatal(err)
	}
	if p == nil {
		return id
	}
	status := p.status
	if status == "" {
		status = "located"
	}
	err := db.Exec(`INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, position, city, city_key, city_code, district, district_key, state, latitude, longitude, geo_precision, geo_status, fingerprint, created_at, updated_at)
		VALUES (?, ?, ?, 'home', true, 0, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), 'SP', ?, ?, NULLIF(?, ''), ?, ?, ?, ?)`,
		uuid.NewString(), ws, id, p.city, p.cityKey, p.cityCode, p.district, p.districtK, p.lat, p.lng, p.precision, status, id[:32], now, now).Error
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func paulistaAddress(precision string) *seededPosition {
	lat, lng := at(-23.5614, -46.6559)
	return &seededPosition{lat: lat, lng: lng, precision: precision, city: "São Paulo", cityKey: "3550308", district: "Bela Vista", districtK: "bela vista"}
}

func TestAPositionIsReadAsLatitudeAndLongitudeAndFoundOnlyWhereItIsAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	ws := uuid.NewString()
	id := seedLead(t, db, ws, paulistaAddress("street"))
	reader := NewMapReader(db)
	ctx := context.Background()

	here, err := geo.SnapWindow(geo.BBox{South: -23.57, West: -46.67, North: -23.55, East: -46.64}, 15)
	if err != nil {
		t.Fatal(err)
	}
	points := layerIn(t, db, ws, crmfilter.Filter{}, here).Points
	if len(points) != 1 || points[0].Position != (geo.Point{Lat: -23.5614, Lng: -46.6559}) || points[0].LeadIDs[0] != id {
		t.Fatalf("points = %+v, want Avenida Paulista as (lat, lng)", points)
	}
	swapped, err := geo.SnapWindow(geo.BBox{South: -46.67, West: -23.57, North: -46.64, East: -23.55}, 15)
	if err != nil {
		t.Fatal(err)
	}
	if none := layerIn(t, db, ws, crmfilter.Filter{}, swapped); none.Kind != leadmap.LayerPoints || len(none.Points) != 0 {
		t.Fatalf("a window with the axes swapped finds nothing: %+v", none)
	}
	ids, total, err := reader.LeadsAt(ctx, leadmap.Scope{WorkspaceID: ws}, geo.Point{Lat: -23.5614, Lng: -46.6559}, crmfilter.PlacementOnMap, leadmap.MaxPeekLeads)
	if err != nil || total != 1 || len(ids) != 1 || ids[0] != id {
		t.Fatalf("peek = %v %d %v", ids, total, err)
	}
}

func TestTheLayerDrawsApproximateLeadsAtTheirReferencePointApartFromThePreciseOnesAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	ws := uuid.NewString()
	street := seedLead(t, db, ws, paulistaAddress("street"))
	bairro := seedLead(t, db, ws, paulistaAddress("district"))
	cep := seedLead(t, db, ws, paulistaAddress("postal_code"))
	cityLat, cityLng := at(-23.5505, -46.6333)
	city := seedLead(t, db, ws, &seededPosition{lat: cityLat, lng: cityLng, precision: "city", city: "São Paulo", cityKey: "3550308"})
	seedLead(t, db, ws, &seededPosition{status: "pending", city: "São Paulo", cityKey: "3550308"})
	seedLead(t, db, ws, nil)
	reader := NewMapReader(db)
	ctx := context.Background()

	summary, err := reader.Summary(ctx, leadmap.Scope{WorkspaceID: ws})
	if err != nil {
		t.Fatal(err)
	}
	w, err := geo.SnapWindow(geo.BBox{South: -23.6, West: -46.7, North: -23.5, East: -46.6}, 13)
	if err != nil {
		t.Fatal(err)
	}
	layer := layerIn(t, db, ws, crmfilter.Filter{}, w)
	if layer.Kind != leadmap.LayerPoints || len(layer.Points) != 3 {
		t.Fatalf("points = %+v, want the street pin, the shared CEP and bairro point, and the city point", layer.Points)
	}
	people := map[crmfilter.GeoPlacement]int64{}
	byID := map[string]leadmap.Point{}
	for _, p := range layer.Points {
		people[p.Placement] += int64(p.People)
		byID[p.ID()] = p
	}
	if people[crmfilter.PlacementOnMap] != summary.OnMap || people[crmfilter.PlacementApproximate] != summary.Approximate {
		t.Fatalf("points hold %v, the summary counts %d on the map and %d approximate", people, summary.OnMap, summary.Approximate)
	}
	pin := byID["-23.5614,-46.6559"]
	shared := byID["approximate:-23.5614,-46.6559"]
	if pin.Placement != crmfilter.PlacementOnMap || pin.People != 1 || pin.LeadIDs[0] != street || pin.Precision != geo.PrecisionStreet {
		t.Fatalf("street pin = %+v", pin)
	}
	if shared.Placement != crmfilter.PlacementApproximate || shared.People != 2 || shared.Precision != geo.PrecisionPostalCode {
		t.Fatalf("a precise and an approximate lead at one position must stay two points, got %+v", shared)
	}
	if far := byID["approximate:-23.5505,-46.6333"]; far.People != 1 || far.LeadIDs[0] != city || far.Precision != geo.PrecisionCity {
		t.Fatalf("city point = %+v", far)
	}

	pinned, total, err := reader.LeadsAt(ctx, leadmap.Scope{WorkspaceID: ws}, geo.Point{Lat: -23.5614, Lng: -46.6559}, crmfilter.PlacementOnMap, leadmap.MaxPeekLeads)
	if err != nil || total != 1 || pinned[0] != street {
		t.Fatalf("precise peek = %v %d %v", pinned, total, err)
	}
	near, total, err := reader.LeadsAt(ctx, leadmap.Scope{WorkspaceID: ws}, geo.Point{Lat: -23.5614, Lng: -46.6559}, crmfilter.PlacementApproximate, leadmap.MaxPeekLeads)
	if err != nil || total != 2 || !sameIDs(near, []string{bairro, cep}) {
		t.Fatalf("approximate peek = %v %d %v", near, total, err)
	}

	cells, err := reader.Layer(ctx, leadmap.Scope{WorkspaceID: ws}, leadmap.LayerRequest{Claim: w.Claim(), MaxPoints: 0, CellSize: w.CellSize(geo.MaxCellsPerAxis)})
	if err != nil || cells.Kind != leadmap.LayerCells {
		t.Fatalf("cells = %+v %v", cells, err)
	}
	inCells := map[crmfilter.GeoPlacement]int64{}
	for _, c := range cells.Cells {
		inCells[c.Placement] += int64(c.People)
	}
	if inCells[crmfilter.PlacementOnMap] != summary.OnMap || inCells[crmfilter.PlacementApproximate] != summary.Approximate {
		t.Fatalf("cells hold %v, the summary counts %d on the map and %d approximate", inCells, summary.OnMap, summary.Approximate)
	}
}

func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	for _, id := range want {
		if !seen[id] {
			return false
		}
	}
	return true
}

func TestCellsUseTheSameMathAsTheDomainAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	ws := uuid.NewString()
	positions := [][2]float64{{-23.5614, -46.6559}, {-23.5615, -46.6560}, {-23.5489, -46.6388}, {-22.9068, -43.1729}}
	for _, p := range positions {
		lat, lng := at(p[0], p[1])
		seedLead(t, db, ws, &seededPosition{lat: lat, lng: lng, precision: "exact"})
	}
	w, err := geo.SnapWindow(geo.BBox{South: -24, West: -47, North: -22.5, East: -43}, 7)
	if err != nil {
		t.Fatal(err)
	}
	size := geo.CellSizeDegrees(w.Zoom())
	layer, err := NewMapReader(db).Layer(context.Background(), leadmap.Scope{WorkspaceID: ws}, leadmap.LayerRequest{Claim: w.Claim(), MaxPoints: 0, CellSize: size})
	if err != nil || layer.Kind != leadmap.LayerCells {
		t.Fatalf("layer = %+v %v, want cells once the positions pass the switch", layer, err)
	}
	cells := layer.Cells
	want := map[[2]int64]int{}
	for _, p := range positions {
		ix, iy := geo.CellIndex(geo.Point{Lat: p[0], Lng: p[1]}, size)
		want[[2]int64{ix, iy}]++
	}
	if len(cells) != len(want) {
		t.Fatalf("cells = %+v, want %v", cells, want)
	}
	for _, c := range cells {
		if want[[2]int64{c.IX, c.IY}] != c.People {
			t.Fatalf("cell (%d, %d) holds %d, want %d", c.IX, c.IY, c.People, want[[2]int64{c.IX, c.IY}])
		}
	}
}

func TestPointsPastFiveThousandPositionsAreReportedSoTheLayerBecomesCellsAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	ws := uuid.NewString()
	err := db.Exec(`WITH made AS (
			INSERT INTO leads (id, workspace_id, name, created_at, updated_at)
			SELECT gen_random_uuid(), ?, 'Lead ' || i, now(), now() FROM generate_series(1, ?) i RETURNING id
		)
		INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, position, latitude, longitude, geo_precision, geo_status, fingerprint, created_at, updated_at)
		SELECT gen_random_uuid(), ?, id, 'home', true, 0, -23.5 - (row_number() OVER ()) * 0.00001, -46.6, 'street', 'located', left(md5(id::text), 32), now(), now() FROM made`,
		ws, leadmap.MaxPoints+1, ws).Error
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"leads", "lead_addresses"} {
		if err := db.Exec("ANALYZE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	w, err := geo.SnapWindow(geo.BBox{South: -23.6, West: -46.7, North: -23.4, East: -46.5}, 11)
	if err != nil {
		t.Fatal(err)
	}
	cells := layerIn(t, db, ws, crmfilter.Filter{}, w)
	if cells.Kind != leadmap.LayerCells {
		t.Fatalf("5,001 distinct positions = %s, want cells", cells.Kind)
	}
	people := 0
	for _, c := range cells.Cells {
		people += c.People
	}
	if people != leadmap.MaxPoints+1 {
		t.Fatalf("the cells hold %d people, want every one of the %d", people, leadmap.MaxPoints+1)
	}
	if err := db.Exec(`UPDATE leads SET deleted_at = now(), version = version + 1 WHERE id = (SELECT id FROM leads WHERE workspace_id = ? LIMIT 1)`, ws).Error; err != nil {
		t.Fatal(err)
	}
	points := layerIn(t, db, ws, crmfilter.Filter{}, w)
	if points.Kind != leadmap.LayerPoints || len(points.Points) != leadmap.MaxPoints {
		t.Fatalf("5,000 positions = %s with %d points, want every position as a point", points.Kind, len(points.Points))
	}
}

func boundToAreas(t *testing.T, ws, viewer string, areas leadarea.Repository, ids ...string) crmfilter.Filter {
	t.Helper()
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldArea, Operator: crmfilter.OpIn, Values: ids},
	}}}}
	found, err := areas.FindLive(context.Background(), ws, ids)
	if err != nil {
		t.Fatal(err)
	}
	bound, _, err := leadarea.Bind(f, ws, viewer, found)
	if err != nil {
		t.Fatal(err)
	}
	return bound
}

func TestAnExactOnlyAreaHoldsOnlyLeadsWhosePositionPinsAHouseAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	ws, other := uuid.NewString(), uuid.NewString()
	owner := uuid.NewString()
	street := seedLead(t, db, ws, paulistaAddress("street"))
	seedLead(t, db, ws, paulistaAddress("district"))
	seedLead(t, db, ws, paulistaAddress("city"))
	outsideLat, outsideLng := at(-19.92, -43.94)
	seedLead(t, db, ws, &seededPosition{lat: outsideLat, lng: outsideLng, precision: "exact"})
	seedLead(t, db, ws, &seededPosition{status: "pending", city: "São Paulo", cityKey: "3550308"})
	seedLead(t, db, ws, nil)
	seedLead(t, db, other, paulistaAddress("exact"))

	areas := leadarea_repository.NewRepository(db)
	area, err := leadarea.New(ws, owner, leadarea.Draft{Name: "Paulista", Shape: geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -23.5614, Lng: -46.6559}, RadiusM: 1000}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	area.ID = uuid.NewString()
	if err := areas.Create(context.Background(), area); err != nil {
		t.Fatal(err)
	}
	reader := NewMapReader(db)
	inArea := exactAreas(t, ws, owner, areas, area.ID)
	summary, err := reader.Summary(context.Background(), leadmap.Scope{WorkspaceID: ws, Filter: inArea})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 1 || summary.OnMap != 1 {
		t.Fatalf("area summary = %+v, want only the street-level lead: centroids never fall inside a drawn area", summary)
	}
	w, _ := geo.SnapWindow(geo.BBox{South: -23.6, West: -46.7, North: -23.5, East: -46.6}, 13)
	if points := layerIn(t, db, ws, inArea, w).Points; len(points) != 1 || points[0].LeadIDs[0] != street {
		t.Fatalf("area points = %+v", points)
	}

	whole, err := reader.Summary(context.Background(), leadmap.Scope{WorkspaceID: ws})
	if err != nil {
		t.Fatal(err)
	}
	want := leadmap.Summary{Total: 6, OnMap: 2, Approximate: 2, WithoutAddress: 1, Pending: 1}
	if whole != want {
		t.Fatalf("workspace summary = %+v, want %+v", whole, want)
	}
	if whole.OnMap+whole.Approximate+whole.WithoutAddress+whole.NotFound+whole.Pending+whole.QuotaExceeded+whole.Refused != whole.Total {
		t.Fatalf("the buckets must partition the total: %+v", whole)
	}

	if err := areas.Delete(context.Background(), ws, area.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	found, err := areas.FindLive(context.Background(), ws, []string{area.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := leadarea.Bind(crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldArea, Operator: crmfilter.OpIn, Values: []string{area.ID}}}}}}, ws, owner, found); err == nil {
		t.Fatal("a deleted area must be refused before any filter compiles")
	}
	stale, err := reader.Summary(context.Background(), leadmap.Scope{WorkspaceID: ws, Filter: inArea})
	if err != nil || stale.Total != 0 {
		t.Fatalf("even a filter bound before the delete matches nobody once the area is gone: %+v %v", stale, err)
	}
}

func TestAColouredLayerInsideAnAreaTakesTheCommonestValueOfItsOwnLeadsAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	ws, other, owner := uuid.NewString(), uuid.NewString(), uuid.NewString()
	classify := func(id, value string) {
		if err := db.Exec(`UPDATE leads SET custom_fields = jsonb_build_object('classificacao', ?::text) WHERE id = ?`, value, id).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"Positivo", "Positivo", "Negativo"} {
		classify(seedLead(t, db, ws, paulistaAddress("street")), value)
	}
	for range 3 {
		classify(seedLead(t, db, other, paulistaAddress("street")), "Negativo")
	}
	areas := leadarea_repository.NewRepository(db)
	area, err := leadarea.New(ws, owner, leadarea.Draft{Name: "Paulista", Shape: geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -23.5614, Lng: -46.6559}, RadiusM: 1000}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	area.ID = uuid.NewString()
	if err := areas.Create(context.Background(), area); err != nil {
		t.Fatal(err)
	}
	w, _ := geo.SnapWindow(geo.BBox{South: -23.6, West: -46.7, North: -23.5, East: -46.6}, 13)
	q := leadmap.NewLayerRequest(w, nil)
	q.ColorByKey = "classificacao"
	layer, err := NewMapReader(db).Layer(context.Background(), leadmap.Scope{WorkspaceID: ws, Filter: boundToAreas(t, ws, owner, areas, area.ID)}, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(layer.Points) != 1 || layer.Points[0].People != 3 || layer.Points[0].Value != "Positivo" {
		t.Fatalf("coloured points = %+v, want the three leads of this workspace coloured by their commonest value", layer.Points)
	}
}

func TestDistrictsCountApproximateLeadsAndPlaceTheBairroAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	ws := uuid.NewString()
	seedLead(t, db, ws, paulistaAddress("street"))
	seedLead(t, db, ws, paulistaAddress("district"))
	cityLat, cityLng := at(-23.0, -46.0)
	seedLead(t, db, ws, &seededPosition{lat: cityLat, lng: cityLng, precision: "city", city: "São Paulo", cityKey: "3550308", district: "bela vista", districtK: "bela vista"})
	seedLead(t, db, ws, &seededPosition{status: "pending", city: "São Paulo", cityKey: "3550308", district: "Bela Vista", districtK: "bela vista"})
	seedLead(t, db, ws, &seededPosition{status: "pending", city: "São Paulo", cityKey: "3550308", district: "Sé", districtK: "se"})
	districts, err := NewMapReader(db).Districts(context.Background(), leadmap.Scope{WorkspaceID: ws}, leadmap.MaxDistricts)
	if err != nil {
		t.Fatal(err)
	}
	if len(districts) != 1 {
		t.Fatalf("districts = %+v, want only the bairro that can be placed", districts)
	}
	d := districts[0]
	if d.People != 4 || d.Name != "Bela Vista" || d.CityKey != "3550308" || d.DistrictKey != "bela vista" {
		t.Fatalf("district = %+v, want every lead of the pair and the commonest spelling", d)
	}
	if math.Abs(d.Position.Lat+23.5614) > 1e-9 || math.Abs(d.Position.Lng+46.6559) > 1e-9 {
		t.Fatalf("the bairro point %+v must ignore the city centroid", d.Position)
	}
	top, err := NewMapReader(db).TopCity(context.Background(), leadmap.Scope{WorkspaceID: ws})
	if err != nil || top == nil || top.CityKey != "3550308" || top.People != 5 || top.Name != "São Paulo" {
		t.Fatalf("top city = %+v %v", top, err)
	}
	extent, err := NewMapReader(db).Extent(context.Background(), leadmap.Scope{WorkspaceID: ws})
	if err != nil || extent == nil || extent.South != -23.5614 || extent.North != -23.0 {
		t.Fatalf("extent = %+v %v", extent, err)
	}
}

func explainMS(t *testing.T, db *gorm.DB, m mapQuery, sql string, args []interface{}) float64 {
	t.Helper()
	var raw string
	err := inReadSession(context.Background(), db, m.settings(), func(tx *gorm.DB) error {
		return tx.Raw("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+sql, args...).Row().Scan(&raw)
	})
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	var plans []struct {
		Execution float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) == 0 {
		t.Fatalf("explain output: %v %s", err, raw)
	}
	return plans[0].Execution
}

func explainPlan(t *testing.T, db *gorm.DB, m mapQuery, sql string, args []interface{}) string {
	t.Helper()
	var lines []string
	err := inReadSession(context.Background(), db, m.settings(), func(tx *gorm.DB) error {
		return tx.Raw("EXPLAIN (ANALYZE, BUFFERS) "+sql, args...).Scan(&lines).Error
	})
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	return strings.Join(lines, "\n")
}

func seedWorkspace(t *testing.T, db *gorm.DB, ws string, leads int) {
	t.Helper()
	err := db.Exec(`INSERT INTO leads (id, workspace_id, number, name, blocked, custom_fields, created_at, updated_at)
		SELECT gen_random_uuid(), ?, '55' || (11000000000 + i)::text, 'Lead ' || i, i % 50 = 0,
			jsonb_build_object('classificacao', (ARRAY['Positivo', 'Negativo', 'A conquistar', 'Não informado'])[1 + i % 4]),
			now(), now() FROM generate_series(1, ?) i`, ws, leads).Error
	if err != nil {
		t.Fatal(err)
	}
	err = db.Exec(`INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, position, city, city_key, city_code, district, district_key, state,
			latitude, longitude, geo_precision, geo_status, fingerprint, created_at, updated_at)
		SELECT gen_random_uuid(), s.workspace_id, s.id, 'home', true, 0,
			c.city, c.city_key, CASE WHEN s.n % 5 = 0 THEN c.code END, 'Bairro ' || (s.n % 400), 'bairro ' || (s.n % 400), c.state,
			CASE WHEN s.n % 100 < 75 THEN round((c.lat + (random() - 0.5) * c.span)::numeric, 4)::float8 END,
			CASE WHEN s.n % 100 < 75 THEN round((c.lng + (random() - 0.5) * c.span)::numeric, 4)::float8 END,
			CASE WHEN s.n % 100 < 60 THEN 'street' WHEN s.n % 100 < 70 THEN 'district' WHEN s.n % 100 < 75 THEN 'city' END,
			CASE WHEN s.n % 100 < 75 THEN 'located' WHEN s.n % 100 < 80 THEN 'not_found' ELSE 'pending' END,
			left(md5(s.id::text), 32), now(), now()
		FROM (SELECT id, workspace_id, row_number() OVER () AS n FROM leads WHERE workspace_id = ?) s
		CROSS JOIN LATERAL (SELECT * FROM (VALUES
			(0, 'São Paulo', 'sp:sao paulo', '3550308', 'SP', -23.55, -46.63, 0.5),
			(1, 'Belo Horizonte', 'mg:belo horizonte', '3106200', 'MG', -19.92, -43.94, 0.25),
			(2, 'Rio de Janeiro', 'rj:rio de janeiro', '3304557', 'RJ', -22.90, -43.20, 0.4)
		) v(k, city, city_key, code, state, lat, lng, span) WHERE v.k = CASE WHEN s.n % 20 < 12 THEN 0 WHEN s.n % 20 < 17 THEN 1 ELSE 2 END) c
		WHERE s.n % 100 < 90`, ws).Error
	if err != nil {
		t.Fatal(err)
	}
}

const (
	warmBudgetMS  = 300.0
	warmCeilingMS = 2 * warmBudgetMS
	firstCeiling  = 1500.0
)

type budgetProbe struct {
	name string
	run  func() float64
}

func wallMS(t *testing.T, call func() error) float64 {
	t.Helper()
	started := time.Now()
	if err := call(); err != nil {
		t.Fatalf("probe call: %v", err)
	}
	return float64(time.Since(started).Microseconds()) / 1000
}

func TestMapSectionsStayInsideTheirBudgetOnAWorkspaceOf188kLeadsAgainstPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("generates 188k leads")
	}
	db := mapDB(t)
	volumeTables(t, db)
	volumeIndexes(t, db)
	ws, noise := uuid.NewString(), uuid.NewString()
	seedWorkspace(t, db, ws, 188000)
	seedWorkspace(t, db, noise, 60000)
	seedReferenceVolume(t, db)
	if err := db.Exec("VACUUM ANALYZE leads").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("VACUUM ANALYZE lead_addresses").Error; err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	areas := leadarea_repository.NewRepository(db)
	small, _ := leadarea.New(ws, owner, leadarea.Draft{Name: "Paulista", Visibility: shared.VisibilityShared, Shape: geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -23.5614, Lng: -46.6559}, RadiusM: 1000}}, time.Now())
	city, _ := leadarea.New(ws, owner, leadarea.Draft{Name: "São Paulo", Visibility: shared.VisibilityShared, Shape: geo.Shape{Kind: geo.ShapePolygon, Ring: []geo.Point{
		{Lat: -23.80, Lng: -46.88}, {Lat: -23.80, Lng: -46.38}, {Lat: -23.30, Lng: -46.38}, {Lat: -23.30, Lng: -46.88},
	}}}, time.Now())
	for _, a := range []*leadarea.Area{&small, &city} {
		a.ID = uuid.NewString()
		if err := areas.Create(context.Background(), *a); err != nil {
			t.Fatal(err)
		}
	}

	street, _ := geo.SnapWindow(geo.BBox{South: -23.575, West: -46.675, North: -23.548, East: -46.635}, 15)
	wholeCity, _ := geo.SnapWindow(geo.BBox{South: -23.80, West: -46.88, North: -23.30, East: -46.38}, 10)
	country, _ := geo.SnapWindow(geo.BBox{South: -33.8, West: -74.1, North: 5.3, East: -28.8}, 4)
	reader := &mapReader{db: db, leads: &repository{db: db, agg: newAggregateCache(nil)}}
	scope := func(f crmfilter.Filter) mapQuery {
		m, err := reader.scope(leadmap.Scope{WorkspaceID: ws, Filter: f})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	inSmallFilter := boundToAreas(t, ws, owner, areas, small.ID)
	inCityFilter := boundToAreas(t, ws, owner, areas, city.ID)
	everyone, inSmall, inCity := scope(crmfilter.Filter{}), scope(inSmallFilter), scope(inCityFilter)
	colored := func(w geo.Window) leadmap.LayerRequest {
		q := leadmap.NewLayerRequest(w, nil)
		q.ColorByKey = "classificacao"
		return q
	}
	var spot struct {
		Lat, Lng float64
	}
	if err := db.Raw(`SELECT latitude AS lat, longitude AS lng FROM lead_addresses WHERE workspace_id = ? AND geo_precision = 'street' LIMIT 1`, ws).Scan(&spot).Error; err != nil {
		t.Fatal(err)
	}

	var probes []budgetProbe
	explained := func(name string, m mapQuery, build func() (string, []interface{})) {
		sql, args := build()
		probes = append(probes, budgetProbe{name, func() float64 { return explainMS(t, db, m, sql, args) }})
	}
	called := func(name string, call func() error) {
		probes = append(probes, budgetProbe{name, func() float64 { return wallMS(t, call) }})
	}
	layer := func(m mapQuery, q leadmap.LayerRequest) func() (string, []interface{}) {
		return func() (string, []interface{}) { return m.layerCells(q) }
	}
	explained("summary, whole workspace", everyone, everyone.summary)
	explained("summary, small area (1 km circle)", inSmall, inSmall.summary)
	explained("summary, whole-city area", inCity, inCity.summary)
	explained("layer, street window z15 (points)", everyone, layer(everyone, leadmap.NewLayerRequest(street, nil)))
	explained("layer, street window z15 inside the small area", inSmall, layer(inSmall, leadmap.NewLayerRequest(street, nil)))
	explained("layer, whole city z10 (cells)", everyone, layer(everyone, leadmap.NewLayerRequest(wholeCity, nil)))
	explained("layer, whole city z10 inside the whole-city area", inCity, layer(inCity, leadmap.NewLayerRequest(wholeCity, nil)))
	explained("layer, country z4 (cells)", everyone, layer(everyone, leadmap.NewLayerRequest(country, nil)))
	explained("layer points, street window z15", everyone, func() (string, []interface{}) { return everyone.layerPoints(leadmap.NewLayerRequest(street, nil)) })
	explained("layer points coloured, street window z15", everyone, func() (string, []interface{}) { return everyone.layerPoints(colored(street)) })
	explained("layer coloured, street window z15", everyone, layer(everyone, colored(street)))
	explained("layer coloured, whole city z10", everyone, layer(everyone, colored(wholeCity)))
	explained("layer coloured, country z4", everyone, layer(everyone, colored(country)))
	explained("layer coloured, street window z15 inside the small area", inSmall, layer(inSmall, colored(street)))
	explained("layer coloured, whole city z10 inside the whole-city area", inCity, layer(inCity, colored(wholeCity)))
	explained("districts, whole workspace", everyone, func() (string, []interface{}) { return everyone.districts(leadmap.MaxDistricts) })
	explained("districts, inside the whole-city area", inCity, func() (string, []interface{}) { return inCity.districts(leadmap.MaxDistricts) })
	explained("viewport extent", everyone, everyone.extent)
	explained("viewport top city", everyone, everyone.topCity)
	explained("peek at one position", everyone, func() (string, []interface{}) {
		sql, args, err := everyone.leadsAt(geo.Point{Lat: spot.Lat, Lng: spot.Lng}, crmfilter.PlacementOnMap, leadmap.MaxPeekLeads)
		if err != nil {
			t.Fatal(err)
		}
		return sql, args
	})
	leftOutSmallFilter, _, err := leadarea.LeftOut(inSmallFilter)
	if err != nil {
		t.Fatal(err)
	}
	leftOutCityFilter, _, err := leadarea.LeftOut(inCityFilter)
	if err != nil {
		t.Fatal(err)
	}
	leftOutSmall, leftOutCity := scope(leftOutSmallFilter), scope(leftOutCityFilter)
	explained("left out total, small area", leftOutSmall, leftOutSmall.leftOutTotal)
	explained("left out bairros, small area", leftOutSmall, func() (string, []interface{}) { return leftOutSmall.leftOutDistricts(leadmap.MaxLeftOutDistricts) })
	explained("left out total, whole-city area", leftOutCity, leftOutCity.leftOutTotal)
	explained("left out bairros, whole-city area", leftOutCity, func() (string, []interface{}) { return leftOutCity.leftOutDistricts(leadmap.MaxLeftOutDistricts) })
	approximate := scope(placementFilter(crmfilter.OpIn, crmfilter.PlacementApproximate, crmfilter.PlacementPending))
	explained("summary, approximate or pending placement", approximate, approximate.summary)
	repo := reader.leads
	for _, f := range []struct {
		name   string
		filter crmfilter.Filter
	}{{"no filter", crmfilter.Filter{}}, {"whole-city area", inCityFilter}, {"small area", inSmallFilter}} {
		input := lead.ListLeadsInput{WorkspaceID: ws, Filter: f.filter, Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 50}}}
		section := lead.SectionQuery{WorkspaceID: ws, Filter: f.filter, Today: time.Now()}
		called("list page 1, "+f.name, func() error { _, err := repo.ListWithSummary(input); return err })
		called("list summary section, "+f.name, func() error { _, err := repo.ReadSummary(context.Background(), section); return err })
		called("list facets section, "+f.name, func() error { _, err := repo.ReadFacets(context.Background(), section); return err })
		called("list places section, "+f.name, func() error { _, err := repo.ReadPlaces(context.Background(), section); return err })
	}

	var over []string
	for _, p := range probes {
		first := p.run()
		warm := math.Inf(1)
		for i := 0; i < 3; i++ {
			warm = math.Min(warm, p.run())
		}
		t.Logf("%-52s first %8.1f ms, best warm %8.1f ms", p.name, first, warm)
		if warm > warmCeilingMS || first > firstCeiling {
			over = append(over, p.name)
		}
	}
	for _, coloured := range []struct {
		name string
		m    mapQuery
		q    leadmap.LayerRequest
	}{{"small area", inSmall, colored(street)}, {"whole-city area", inCity, colored(wholeCity)}} {
		sql, args := coloured.m.layerPoints(coloured.q)
		if plan := explainPlan(t, db, coloured.m, sql, args); strings.Contains(plan, "Seq Scan on leads lc") {
			over = append(over, "layer coloured inside the "+coloured.name+" scans every lead of every workspace")
			t.Logf("plan of the coloured layer inside the %s:\n%s", coloured.name, plan)
		}
	}
	if plan := os.Getenv("VOZKO_MAP_PLAN"); plan != "" {
		for _, probe := range []struct {
			name string
			m    mapQuery
			b    func() (string, []interface{})
		}{{"summary, whole-city area", inCity, inCity.summary}, {"layer, whole city z10", everyone, layer(everyone, colored(wholeCity))}} {
			if strings.Contains(probe.name, plan) {
				sql, args := probe.b()
				t.Logf("plan of %s:\n%s", probe.name, explainPlan(t, db, probe.m, sql, args))
			}
		}
	}
	assertReferencePlacesAtVolume(t, db, ws)
	if len(over) > 0 {
		t.Fatalf("over the ceiling (best warm above %.0f ms or first above %.0f ms) or on a plan that reads every workspace: %s", warmCeilingMS, firstCeiling, strings.Join(over, "; "))
	}
}

func assertReferencePlacesAtVolume(t *testing.T, db *gorm.DB, ws string) {
	t.Helper()
	reader := NewMapReader(db)
	top, err := reader.TopCity(context.Background(), leadmap.Scope{WorkspaceID: ws})
	if err != nil || top == nil || top.CityKey != "sp:sao paulo" || top.Center == nil || *top.Center != (geo.Point{Lat: -23.55, Lng: -46.63}) {
		t.Fatalf("top city at volume = %+v %v, want São Paulo centred by its name although most addresses carry no IBGE code", top, err)
	}
	var references []struct {
		CityCode, DistrictKey string
		Latitude, Longitude   float64
	}
	if err := db.Raw(`SELECT city_code, district_key, latitude, longitude FROM geo_district_points`).Scan(&references).Error; err != nil {
		t.Fatal(err)
	}
	codes := map[string]string{"sp:sao paulo": "3550308", "mg:belo horizonte": "3106200", "rj:rio de janeiro": "3304557"}
	points := map[string]geo.Point{}
	for _, r := range references {
		points[r.CityCode+"/"+r.DistrictKey] = geo.Point{Lat: r.Latitude, Lng: r.Longitude}
	}
	var seeded int
	if err := db.Raw(`SELECT COUNT(DISTINCT (city_key, district_key)) FROM lead_addresses WHERE workspace_id = ? AND is_primary AND district_key IS NOT NULL`, ws).Scan(&seeded).Error; err != nil {
		t.Fatal(err)
	}
	districts, err := reader.Districts(context.Background(), leadmap.Scope{WorkspaceID: ws}, leadmap.MaxDistricts)
	if err != nil || seeded == 0 || len(districts) != seeded {
		t.Fatalf("districts at volume = %d %v, want every one of the %d seeded bairros, each with a reference point", len(districts), err, seeded)
	}
	for _, d := range districts {
		if want, ok := points[codes[d.CityKey]+"/"+d.DistrictKey]; !ok || d.Position != want {
			t.Fatalf("%s at %+v, want its IBGE reference point %+v", d.Pair(), d.Position, want)
		}
	}
}

func seedReferencePoints(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now()
	if err := db.Exec(`INSERT INTO geo_cities (city_code, name, name_key, state, latitude, longitude, built_at) VALUES ('3550308', 'São Paulo', 'sao paulo', 'SP', -23.5329, -46.6395, ?)`, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO geo_district_points (city_code, district_key, name, latitude, longitude, spread_m, sample_count, source, built_at) VALUES
		('3550308', 'se', 'Sé', -23.5503, -46.6339, 900, 4000, 'cnefe', ?),
		('3550308', 'bela vista', 'Bela Vista', -23.5600, -46.6500, 1200, 9000, 'cnefe', ?)`, now, now).Error; err != nil {
		t.Fatal(err)
	}
}

func spAddress(district, districtKey string, p *seededPosition) *seededPosition {
	if p == nil {
		p = &seededPosition{status: "pending"}
	}
	p.city, p.cityKey, p.district, p.districtK = "São Paulo", "sp:sao paulo", district, districtKey
	return p
}

func TestBairrosTakeTheReferencePointAndAppearWithoutLocatedLeadsAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	seedReferencePoints(t, db)
	ws := uuid.NewString()
	streetLat, streetLng := at(-23.5614, -46.6559)
	seedLead(t, db, ws, spAddress("Bela Vista", "bela vista", &seededPosition{lat: streetLat, lng: streetLng, precision: "street"}))
	seedLead(t, db, ws, spAddress("Sé", "se", nil))
	seedLead(t, db, ws, spAddress("Sé", "se", nil))
	misspelt := &seededPosition{status: "pending", city: "Sao Paolo", cityKey: "sp:sao paolo", cityCode: "3550308", district: "Sé", districtK: "se"}
	seedLead(t, db, ws, misspelt)
	seedLead(t, db, ws, spAddress("Lugar Nenhum", "lugar nenhum", nil))
	unplacedLat, unplacedLng := at(-23.6, -46.7)
	seedLead(t, db, ws, spAddress("Jardim Novo", "jardim novo", &seededPosition{lat: unplacedLat, lng: unplacedLng, precision: "street"}))

	districts, err := NewMapReader(db).Districts(context.Background(), leadmap.Scope{WorkspaceID: ws}, leadmap.MaxDistricts)
	if err != nil {
		t.Fatal(err)
	}
	byPair := map[string]leadmap.District{}
	for _, d := range districts {
		byPair[d.Pair()] = d
	}
	if len(districts) != 4 {
		t.Fatalf("districts = %+v, want Sé twice (by city key and by IBGE code), Bela Vista and Jardim Novo, never the bairro without any point", districts)
	}
	if _, listed := byPair["sp:sao paulo/lugar nenhum"]; listed {
		t.Fatal("a bairro with no located lead and no reference point cannot be drawn")
	}
	se := byPair["sp:sao paulo/se"]
	if se.People != 2 || se.Position != (geo.Point{Lat: -23.5503, Lng: -46.6339}) {
		t.Fatalf("Sé = %+v, want both leads at the IBGE bairro point although none is located", se)
	}
	if byCode := byPair["sp:sao paolo/se"]; byCode.People != 1 || byCode.Position != (geo.Point{Lat: -23.5503, Lng: -46.6339}) {
		t.Fatalf("a misspelt city with its IBGE code = %+v, want the bairro point found through the code", byCode)
	}
	if bela := byPair["sp:sao paulo/bela vista"]; bela.Position != (geo.Point{Lat: -23.56, Lng: -46.65}) {
		t.Fatalf("Bela Vista = %+v, want the reference point before the mean of its leads", bela)
	}
	if novo := byPair["sp:sao paulo/jardim novo"]; novo.Position != (geo.Point{Lat: -23.6, Lng: -46.7}) {
		t.Fatalf("Jardim Novo = %+v, want the mean of its leads while the reference has no point", novo)
	}

	top, err := NewMapReader(db).TopCity(context.Background(), leadmap.Scope{WorkspaceID: ws})
	if err != nil || top == nil || top.CityKey != "sp:sao paulo" || top.Center == nil || *top.Center != (geo.Point{Lat: -23.5329, Lng: -46.6395}) {
		t.Fatalf("top city = %+v %v, want São Paulo at its IBGE point", top, err)
	}
	if viewport := leadmap.DefaultViewport(nil, top, leadmap.Summary{Total: 6}); viewport.Basis != leadmap.BasisCity {
		t.Fatalf("viewport = %+v, want the city fallback to fire", viewport)
	}

	spelt := uuid.NewString()
	for range 2 {
		seedLead(t, db, spelt, &seededPosition{status: "pending", city: "Sao Paolo", cityKey: "sp:sao paolo", cityCode: "3550308", district: "Sé", districtK: "se"})
	}
	seedLead(t, db, spelt, spAddress("Sé", "se", nil))
	byCode, err := NewMapReader(db).TopCity(context.Background(), leadmap.Scope{WorkspaceID: spelt})
	if err != nil || byCode == nil || byCode.CityKey != "sp:sao paolo" || byCode.Center == nil || *byCode.Center != (geo.Point{Lat: -23.5329, Lng: -46.6395}) {
		t.Fatalf("top city = %+v %v, want the misspelt city centred through its IBGE code", byCode, err)
	}
}

func placementFilter(op crmfilter.Operator, placements ...crmfilter.GeoPlacement) crmfilter.Filter {
	values := make([]string, len(placements))
	for i, p := range placements {
		values[i] = string(p)
	}
	return crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldGeoPlacement, Operator: op, Values: values},
	}}}}
}

func TestEverySummaryBucketIsTheGeoPlacementFilterOfTheSameNameAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	ws, other := uuid.NewString(), uuid.NewString()
	seedLead(t, db, ws, paulistaAddress("street"))
	seedLead(t, db, ws, paulistaAddress("exact"))
	seedLead(t, db, ws, paulistaAddress("district"))
	seedLead(t, db, ws, paulistaAddress("postal_code"))
	seedLead(t, db, ws, nil)
	for _, status := range []string{"not_found", "ambiguous", "quota_exceeded", "refused", "pending", "unavailable", "pending"} {
		seedLead(t, db, ws, &seededPosition{status: status, city: "São Paulo", cityKey: "sp:sao paulo"})
	}
	seedLead(t, db, other, paulistaAddress("street"))
	reader := NewMapReader(db)
	ctx := context.Background()
	summary, err := reader.Summary(ctx, leadmap.Scope{WorkspaceID: ws})
	if err != nil {
		t.Fatal(err)
	}
	want := leadmap.Summary{Total: 12, OnMap: 2, Approximate: 2, WithoutAddress: 1, NotFound: 2, QuotaExceeded: 1, Refused: 1, Pending: 3}
	if summary != want {
		t.Fatalf("summary = %+v, want %+v", summary, want)
	}
	buckets := map[crmfilter.GeoPlacement]int64{
		crmfilter.PlacementOnMap: summary.OnMap, crmfilter.PlacementApproximate: summary.Approximate,
		crmfilter.PlacementWithoutAddress: summary.WithoutAddress, crmfilter.PlacementNotFound: summary.NotFound,
		crmfilter.PlacementPending: summary.Pending, crmfilter.PlacementQuotaExceeded: summary.QuotaExceeded,
		crmfilter.PlacementRefused: summary.Refused,
	}
	for placement, count := range buckets {
		linked, err := reader.Summary(ctx, leadmap.Scope{WorkspaceID: ws, Filter: placementFilter(crmfilter.OpIn, placement)})
		if err != nil {
			t.Fatal(err)
		}
		if linked.Total != count {
			t.Errorf("geo_placement %s lists %d leads, the summary counts %d", placement, linked.Total, count)
		}
		rest, err := reader.Summary(ctx, leadmap.Scope{WorkspaceID: ws, Filter: placementFilter(crmfilter.OpNotIn, placement)})
		if err != nil {
			t.Fatal(err)
		}
		if rest.Total != summary.Total-count {
			t.Errorf("geo_placement not in %s lists %d leads, want %d", placement, rest.Total, summary.Total-count)
		}
	}
	offMap, err := reader.Summary(ctx, leadmap.Scope{WorkspaceID: ws, Filter: placementFilter(crmfilter.OpIn,
		crmfilter.PlacementApproximate, crmfilter.PlacementWithoutAddress, crmfilter.PlacementNotFound, crmfilter.PlacementPending)})
	if err != nil {
		t.Fatal(err)
	}
	if want := summary.Approximate + summary.WithoutAddress + summary.NotFound + summary.Pending; offMap.Total != want {
		t.Fatalf("the Fora do mapa buckets together list %d leads, want %d", offMap.Total, want)
	}
}

func TestTheLeftOutCountListsTheApproximateLeadsInsideTheAreaAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	ws, other := uuid.NewString(), uuid.NewString()
	owner := uuid.NewString()
	paulista := func(precision string) *seededPosition {
		lat, lng := at(-23.5614, -46.6559)
		return spAddress("Bela Vista", "bela vista", &seededPosition{lat: lat, lng: lng, precision: precision})
	}
	seedLead(t, db, ws, paulista("street"))
	seedLead(t, db, ws, paulista("district"))
	seedLead(t, db, ws, paulista("postal_code"))
	farLat, farLng := at(-23.0, -46.0)
	seedLead(t, db, ws, spAddress("Bela Vista", "bela vista", &seededPosition{lat: farLat, lng: farLng, precision: "city"}))
	seedLead(t, db, ws, spAddress("Bela Vista", "bela vista", nil))
	seedLead(t, db, other, paulista("district"))

	areas := leadarea_repository.NewRepository(db)
	area, err := leadarea.New(ws, owner, leadarea.Draft{Name: "Paulista", Shape: geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -23.5614, Lng: -46.6559}, RadiusM: 1000}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	area.ID = uuid.NewString()
	if err := areas.Create(context.Background(), area); err != nil {
		t.Fatal(err)
	}
	left, ok, err := leadarea.LeftOut(exactAreas(t, ws, owner, areas, area.ID))
	if err != nil || !ok {
		t.Fatal("the area filter must have something to leave out")
	}
	reader := NewMapReader(db)
	got, err := reader.LeftOut(context.Background(), leadmap.Scope{WorkspaceID: ws, Filter: left}, leadmap.MaxLeftOutDistricts)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 || len(got.Districts) != 1 || got.Districts[0].Count != 2 || got.Districts[0].Pair != "sp:sao paulo/bela vista" {
		t.Fatalf("left out = %+v, want the bairro and CEP leads inside the ring, never the street lead, the far centroid or the unlocated one", got)
	}
	byBairro := crmfilter.Filter{Groups: append(append([]crmfilter.Group(nil), left.Groups...), crmfilter.Group{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldDistrict, Operator: crmfilter.OpIn, Values: []string{got.Districts[0].Pair}},
	}})}
	listed, err := reader.Summary(context.Background(), leadmap.Scope{WorkspaceID: ws, Filter: byBairro})
	if err != nil || listed.Total != got.Districts[0].Count || listed.Approximate != listed.Total {
		t.Fatalf("the bairro link lists %+v (%v), want the %d left out leads of the bairro, all approximate", listed, err, got.Districts[0].Count)
	}
}

func seedReferenceVolume(t *testing.T, db *gorm.DB) {
	t.Helper()
	err := db.Exec(`INSERT INTO geo_cities (city_code, name, name_key, state, latitude, longitude, built_at) VALUES
		('3550308', 'São Paulo', 'sao paulo', 'SP', -23.55, -46.63, now()),
		('3106200', 'Belo Horizonte', 'belo horizonte', 'MG', -19.92, -43.94, now()),
		('3304557', 'Rio de Janeiro', 'rio de janeiro', 'RJ', -22.90, -43.20, now())`).Error
	if err != nil {
		t.Fatal(err)
	}
	err = db.Exec(`INSERT INTO geo_district_points (city_code, district_key, name, latitude, longitude, spread_m, sample_count, source, built_at)
		SELECT c.city_code, 'bairro ' || n, 'Bairro ' || n, c.latitude + (n % 20 - 10) * 0.01, c.longitude + (n / 20 - 10) * 0.01, 800, 1000, 'cnefe', now()
		FROM geo_cities c CROSS JOIN generate_series(0, 499) n`).Error
	if err != nil {
		t.Fatal(err)
	}
}
