package lead

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/domain/leadmap"
	"vozko/domain/shared"
)

const mapWorkspace = "0b6f9c1e-7d1a-4c61-9a0e-2f8d4c1b2a10"

var pinning = pq.StringArray{"exact", "address", "street"}

func blockedFilter() crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsFalse},
	}}}}
}

func compiledMap(t *testing.T) mapQuery {
	t.Helper()
	q, err := (&repository{}).compile(lead.ListLeadsInput{WorkspaceID: mapWorkspace, Filter: blockedFilter()})
	if err != nil {
		t.Fatal(err)
	}
	return mapQuery{leads: q}
}

func paulistaWindow(t *testing.T) geo.Window {
	t.Helper()
	w, err := geo.SnapWindow(geo.BBox{South: -23.57, West: -46.67, North: -23.55, East: -46.64}, 14)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func assertPlaceholders(t *testing.T, name, sql string, args []interface{}) {
	t.Helper()
	if got := strings.Count(sql, "?"); got != len(args) {
		t.Fatalf("%s binds %d placeholders for %d args:\n%s", name, got, len(args), sql)
	}
}

func layerRequest(t *testing.T, colorBy string) leadmap.LayerRequest {
	t.Helper()
	w := paulistaWindow(t)
	return leadmap.LayerRequest{Claim: w.Claim(), ColorByKey: colorBy, MaxPoints: leadmap.MaxPoints, CellSize: w.CellSize(geo.MaxCellsPerAxis)}
}

func TestTheLayerCountsItsPositionsOnlyUpToOnePastTheSwitch(t *testing.T) {
	m := compiledMap(t)
	q := layerRequest(t, "")
	sql, args := m.layerPositions(q)
	where := "a.workspace_id = ? AND a.is_primary AND a.latitude IS NOT NULL" +
		" AND a.latitude > ? AND a.latitude <= ? AND a.longitude >= ? AND a.longitude < ?" +
		" AND a.lead_id IN (SELECT leads.id FROM leads WHERE " + m.leads.where + ")"
	class := "CASE WHEN a.latitude IS NOT NULL AND a.geo_precision = ANY(?) THEN 'on_map' ELSE 'approximate' END AS placement"
	want := "SELECT count(*) AS positions FROM (SELECT a.latitude, a.longitude, " + class +
		" FROM lead_addresses a WHERE " + where + " GROUP BY 1, 2, 3 LIMIT ?) d"
	if sql != want {
		t.Fatalf("positions sql =\n%s\nwant\n%s", sql, want)
	}
	assertPlaceholders(t, "positions", sql, args)
	c := q.Claim
	wantArgs := append(append([]interface{}{pinning, mapWorkspace, c.South, c.North, c.West, c.East}, m.leads.args...), q.MaxPoints+1)
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("positions args = %#v\nwant      %#v", args, wantArgs)
	}
}

func TestTheLayerGroupsItsAddressesIntoCellsWithoutTouchingLeads(t *testing.T) {
	m := compiledMap(t)
	q := layerRequest(t, "")
	sql, args := m.layerCells(q)
	where := "a.workspace_id = ? AND a.is_primary AND a.latitude IS NOT NULL" +
		" AND a.latitude > ? AND a.latitude <= ? AND a.longitude >= ? AND a.longitude < ?" +
		" AND a.lead_id IN (SELECT leads.id FROM leads WHERE " + m.leads.where + ")"
	class := "CASE WHEN a.latitude IS NOT NULL AND a.geo_precision = ANY(?) THEN 'on_map' ELSE 'approximate' END AS placement"
	want := "SELECT " + class + ", floor(a.longitude / ?::float8)::bigint AS ix, floor(a.latitude / ?::float8)::bigint AS iy," +
		" COUNT(*) AS people, avg(a.latitude) AS lat, avg(a.longitude) AS lng" +
		" FROM lead_addresses a WHERE " + where + " GROUP BY 1, 2, 3"
	if sql != want {
		t.Fatalf("cells sql =\n%s\nwant\n%s", sql, want)
	}
	assertPlaceholders(t, "cells", sql, args)
	c := q.Claim
	wantArgs := append([]interface{}{pinning, q.CellSize, q.CellSize, mapWorkspace, c.South, c.North, c.West, c.East}, m.leads.args...)
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("cells args = %#v\nwant      %#v", args, wantArgs)
	}
}

func TestTheLayerPointsAggregateTheLeadsOfEachPositionAndClass(t *testing.T) {
	m := compiledMap(t)
	q := layerRequest(t, "")
	sql, args := m.layerPoints(q)
	where := "a.workspace_id = ? AND a.is_primary AND a.latitude IS NOT NULL" +
		" AND a.latitude > ? AND a.latitude <= ? AND a.longitude >= ? AND a.longitude < ?" +
		" AND a.lead_id IN (SELECT leads.id FROM leads WHERE " + m.leads.where + ")"
	class := "CASE WHEN a.latitude IS NOT NULL AND a.geo_precision = ANY(?) THEN 'on_map' ELSE 'approximate' END AS placement"
	want := "SELECT a.latitude AS lat, a.longitude AS lng, " + class + ", COUNT(*) AS people," +
		" min(array_position(?::text[], a.geo_precision::text)) AS precision_rank," +
		" (array_agg(a.lead_id::text))[1:5] AS lead_ids," +
		" NULL::text AS value" +
		" FROM lead_addresses a WHERE " + where +
		" GROUP BY 1, 2, 3"
	if sql != want {
		t.Fatalf("points sql =\n%s\nwant\n%s", sql, want)
	}
	assertPlaceholders(t, "points", sql, args)
	c := q.Claim
	wantArgs := append([]interface{}{pinning, pq.StringArray{"exact", "address", "street", "postal_code", "district", "city"}, mapWorkspace, c.South, c.North, c.West, c.East}, m.leads.args...)
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("points args = %#v\nwant      %#v", args, wantArgs)
	}
}

func TestATileLayerReadsOnlyThePositionsItsTileClaims(t *testing.T) {
	m := compiledMap(t)
	tile, err := geo.TileAt(15, 12136, 18589)
	if err != nil {
		t.Fatal(err)
	}
	q := leadmap.NewTileRequest(tile, nil)
	c := tile.Claim()
	claimed := []interface{}{mapWorkspace, c.South, c.North, c.West, c.East}
	cells, cellArgs := m.layerCells(q)
	assertPlaceholders(t, "tile cells", cells, cellArgs)
	if !reflect.DeepEqual(cellArgs[3:8], claimed) || cellArgs[1] != tile.CellSize() || cellArgs[2] != tile.CellSize() {
		t.Fatalf("the tile cells bind %#v, want the tile cell size and its half-open claim %#v", cellArgs, claimed)
	}
	positions, positionArgs := m.layerPositions(q)
	assertPlaceholders(t, "tile positions", positions, positionArgs)
	if !reflect.DeepEqual(positionArgs[1:6], claimed) || positionArgs[len(positionArgs)-1] != leadmap.MaxTilePoints+1 {
		t.Fatalf("the tile positions bind %#v, want its claim and one past the tile cap", positionArgs)
	}
	points, pointArgs := m.layerPoints(q)
	assertPlaceholders(t, "tile points", points, pointArgs)
	if !reflect.DeepEqual(pointArgs[2:7], claimed) {
		t.Fatalf("the tile points bind %#v, want its half-open claim %#v", pointArgs[2:7], claimed)
	}
}

func TestTheLayerColouredByAFieldReadsEachPointLeadByKeyWithoutAJoinOnLeads(t *testing.T) {
	m := compiledMap(t)
	q := layerRequest(t, "classificacao")
	cells, _ := m.layerCells(q)
	if strings.Contains(cells, "leads lc") || strings.Contains(cells, "mode()") || strings.Contains(cells, "array_agg") {
		t.Fatalf("counting positions into cells must not touch leads or aggregate ids:\n%s", cells)
	}
	sql, args := m.layerPoints(q)
	fragment := "mode() WITHIN GROUP (ORDER BY (SELECT lc.custom_fields ->> ?::text FROM leads lc WHERE lc.id = a.lead_id AND lc.workspace_id = a.workspace_id)) AS value"
	if !strings.Contains(sql, fragment) {
		t.Fatalf("coloured points miss %q:\n%s", fragment, sql)
	}
	if strings.Contains(sql, "JOIN leads") {
		t.Fatalf("a join on leads lets an area session hash join the whole table:\n%s", sql)
	}
	assertPlaceholders(t, "coloured points", sql, args)
	if args[2] != "classificacao" {
		t.Fatalf("the colour-by key binds right after the precision ranks, got %#v", args)
	}
}

func TestSummaryBuildsOnTheListSummaryAndAddsTheGeocodingBuckets(t *testing.T) {
	m := compiledMap(t)
	sql, args := m.summary()
	shared, _ := m.leads.addressSummarySQL("", nil)
	head := shared[:strings.Index(shared, " FROM leads")]
	if !strings.HasPrefix(sql, head) {
		t.Fatalf("the map summary must start from the list's address buckets:\n%s\nwant prefix\n%s", sql, head)
	}
	for _, fragment := range []string{
		"COUNT(*) FILTER (WHERE la.lead_id IS NOT NULL AND la.latitude IS NULL AND la.geo_status = ANY(?)) AS not_found",
		"COUNT(*) FILTER (WHERE la.lead_id IS NOT NULL AND la.latitude IS NULL AND la.geo_status = ?) AS quota_exceeded",
		"COUNT(*) FILTER (WHERE la.lead_id IS NOT NULL AND la.latitude IS NULL AND la.geo_status = ?) AS refused",
		"COUNT(*) FILTER (WHERE la.lead_id IS NOT NULL AND la.latitude IS NULL AND NOT COALESCE(la.geo_status = ANY(?), false)) AS pending",
		" FROM leads LEFT JOIN lead_addresses la ON la.lead_id = leads.id AND la.is_primary AND la.workspace_id = leads.workspace_id WHERE " + m.leads.where,
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("summary misses %q:\n%s", fragment, sql)
		}
	}
	assertPlaceholders(t, "summary", sql, args)
	wantHead := []interface{}{pinning, pinning, pq.StringArray{"not_found", "ambiguous"}, "quota_exceeded", "refused", pq.StringArray{"not_found", "ambiguous", "quota_exceeded", "refused"}}
	if !reflect.DeepEqual(args[:6], wantHead) {
		t.Fatalf("summary args = %#v", args)
	}
}

func TestDistrictsExtendTheListPlacesWithTheBairroPoint(t *testing.T) {
	m := compiledMap(t)
	sql, args := m.districts(leadmap.MaxDistricts)
	placed := "la.latitude IS NOT NULL AND la.geo_precision = ANY(?)"
	code := "COALESCE(max(g.city_code), (SELECT gc.city_code FROM geo_cities gc WHERE gc.state = upper(split_part(g.city_key, ':', 1)) AND gc.name_key = substr(g.city_key, strpos(g.city_key, ':') + 1) ORDER BY gc.city_code LIMIT 1))"
	for _, fragment := range []string{
		"SELECT d.city_key, d.district_key, d.district, d.count, COALESCE(dp.latitude, d.lat) AS lat, COALESCE(dp.longitude, d.lng) AS lng FROM (SELECT g.city_key AS city_key, g.district_key AS district_key, (array_agg(g.district ORDER BY g.n DESC, g.district))[1] AS district,",
		"sum(g.n)::bigint AS count, sum(g.lat_sum) / NULLIF(sum(g.placed), 0) AS lat, sum(g.lng_sum) / NULLIF(sum(g.placed), 0) AS lng, " + code + " AS city_code FROM (SELECT ",
		"COUNT(*) AS n, COUNT(*) FILTER (WHERE " + placed + ") AS placed, sum(la.latitude) FILTER (WHERE " + placed + ") AS lat_sum, sum(la.longitude) FILTER (WHERE " + placed + ") AS lng_sum, max(la.city_code) AS city_code FROM lead_addresses la",
		"la.lead_id IN (SELECT leads.id FROM leads WHERE " + m.leads.where + ") AND la.district_key IS NOT NULL",
		"GROUP BY g.city_key, g.district_key HAVING sum(g.placed) > 0 OR EXISTS (SELECT 1 FROM geo_district_points dpx WHERE dpx.city_code = " + code + " AND dpx.district_key = g.district_key) ORDER BY count DESC",
		") d LEFT JOIN geo_district_points dp ON dp.city_code = d.city_code AND dp.district_key = d.district_key ORDER BY d.count DESC, d.city_key, d.district_key",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("districts miss %q:\n%s", fragment, sql)
		}
	}
	assertPlaceholders(t, "districts", sql, args)
	placing := pq.StringArray{"exact", "address", "street", "postal_code", "district"}
	if !reflect.DeepEqual(args[:4], []interface{}{placing, placing, placing, mapWorkspace}) || args[len(args)-1] != leadmap.MaxDistricts {
		t.Fatalf("district args = %#v", args)
	}
}

func TestPlacesCountEachSpellingFirstSoNoAddressIsSortedForTheCommonestName(t *testing.T) {
	m := compiledMap(t)
	cities, _ := m.leads.citiesSQL()
	districts, _ := m.leads.districtsSQL()
	for name, sql := range map[string]string{"cities": cities, "districts": districts} {
		if strings.Contains(sql, "WITHIN GROUP") {
			t.Fatalf("%s use an ordered-set aggregate, which sorts every address of the filter:\n%s", name, sql)
		}
		if !strings.Contains(sql, "COUNT(*) AS n FROM lead_addresses la WHERE ") || !strings.Contains(sql, "(array_agg(g.city ORDER BY g.n DESC, g.city))[1] AS city") {
			t.Fatalf("%s must count each spelling, then keep the commonest:\n%s", name, sql)
		}
	}
	if !strings.Contains(cities, "GROUP BY la.city_key, la.city, la.state) g GROUP BY g.city_key ORDER BY count DESC, g.city_key LIMIT ?") {
		t.Fatalf("cities = %s", cities)
	}
	if !strings.Contains(districts, "GROUP BY la.city_key, la.district_key, la.district, la.city, la.state) g GROUP BY g.city_key, g.district_key ORDER BY count DESC, g.city_key, g.district_key LIMIT ?") {
		t.Fatalf("districts = %s", districts)
	}
}

func TestTheTopCityIsTheFirstOfTheListCitiesWithItsReferencePoint(t *testing.T) {
	m := compiledMap(t)
	sql, args := m.topCity()
	cities, citiesArgs := m.leads.cityCountsSQL(", max(la.city_code) AS city_code", ", max(g.city_code) AS city_code", 1)
	code := "COALESCE(t.city_code, (SELECT gc.city_code FROM geo_cities gc WHERE gc.state = upper(split_part(t.city_key, ':', 1)) AND gc.name_key = substr(t.city_key, strpos(t.city_key, ':') + 1) ORDER BY gc.city_code LIMIT 1))"
	want := "SELECT t.city_key, t.city, t.state, t.count, gcr.latitude AS lat, gcr.longitude AS lng FROM (" + cities + ") t" +
		" LEFT JOIN geo_cities gcr ON gcr.city_code = " + code
	if sql != want || !reflect.DeepEqual(args, citiesArgs) || args[len(args)-1] != 1 {
		t.Fatalf("top city = %s %v\nwant       %s", sql, args, want)
	}
	for _, fragment := range []string{"SELECT la.city_key, la.city, la.state, COUNT(*) AS n, max(la.city_code) AS city_code FROM lead_addresses la", "sum(g.n)::bigint AS count, max(g.city_code) AS city_code FROM ("} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("top city misses %q:\n%s", fragment, sql)
		}
	}
	assertPlaceholders(t, "top city", sql, args)
	listed, _ := m.leads.citiesSQL()
	if strings.Contains(listed, "city_code") {
		t.Fatalf("the list cities section reads no IBGE code:\n%s", listed)
	}
}

func TestMapReaderGivesTheTopCityItsReferencePointWhenOneIsLoaded(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	columns := []string{"city_key", "city", "state", "count", "lat", "lng"}
	expectMapSession(mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT t.city_key, t.city, t.state, t.count")).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("sp:sao paulo", "São Paulo", "SP", 70, -23.55, -46.63))
	mock.ExpectCommit()
	expectMapSession(mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT t.city_key, t.city, t.state, t.count")).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("sp:sao paulo", "São Paulo", "SP", 70, nil, nil))
	mock.ExpectCommit()
	reader := NewMapReader(db)
	top, err := reader.TopCity(context.Background(), leadmap.Scope{WorkspaceID: mapWorkspace})
	if err != nil {
		t.Fatal(err)
	}
	if top == nil || top.Center == nil || *top.Center != (geo.Point{Lat: -23.55, Lng: -46.63}) || top.People != 70 || top.Name != "São Paulo" {
		t.Fatalf("top city = %+v, want São Paulo at its reference point", top)
	}
	unplaced, err := reader.TopCity(context.Background(), leadmap.Scope{WorkspaceID: mapWorkspace})
	if err != nil {
		t.Fatal(err)
	}
	if unplaced == nil || unplaced.Center != nil {
		t.Fatalf("a city without a reference point has no center, got %+v", unplaced)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheLeftOutCountAndBairrosReadTheFilterTheyAreGiven(t *testing.T) {
	m := compiledMap(t)
	total, totalArgs := m.leftOutTotal()
	if total != "SELECT COUNT(*) AS total FROM leads WHERE "+m.leads.where || !reflect.DeepEqual(totalArgs, m.leads.args) {
		t.Fatalf("left out total = %s %#v", total, totalArgs)
	}
	districts, districtArgs := m.leftOutDistricts(leadmap.MaxLeftOutDistricts)
	listed, listedArgs := m.leads.districtCountsSQL("", nil, "", "", leadmap.MaxLeftOutDistricts)
	if districts != listed || !reflect.DeepEqual(districtArgs, listedArgs) {
		t.Fatalf("left out bairros = %s, want the list bairros", districts)
	}
	assertPlaceholders(t, "left out total", total, totalArgs)
	assertPlaceholders(t, "left out bairros", districts, districtArgs)
}

func TestMapReaderCountsTheLeftOutLeadsAndTheirBairrosInOneSession(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectAreaSession(mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) AS total FROM leads WHERE ")).
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(9))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT g.city_key AS city_key, g.district_key AS district_key")).
		WillReturnRows(sqlmock.NewRows([]string{"city_key", "district_key", "district", "city", "state", "count"}).
			AddRow("sp:sao paulo", "jardim paulista", "Jardim Paulista", "São Paulo", "SP", 6))
	mock.ExpectCommit()
	got, err := NewMapReader(db).LeftOut(context.Background(), leadmap.Scope{WorkspaceID: mapWorkspace, Filter: boundApproximateAreaFilter()}, leadmap.MaxLeftOutDistricts)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 9 || len(got.Districts) != 1 {
		t.Fatalf("left out = %+v", got)
	}
	d := got.Districts[0]
	if d.Pair != "sp:sao paulo/jardim paulista" || d.District != "Jardim Paulista" || d.Count != 6 {
		t.Fatalf("left out bairro = %+v, want its filter pair and count", d)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMapReaderFailsTheLeftOutCountWhenEitherReadFails(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectAreaSession(mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) AS total FROM leads WHERE ")).
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(9))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT g.city_key AS city_key")).WillReturnError(errors.New("boom"))
	mock.ExpectRollback()
	if _, err := NewMapReader(db).LeftOut(context.Background(), leadmap.Scope{WorkspaceID: mapWorkspace, Filter: boundApproximateAreaFilter()}, leadmap.MaxLeftOutDistricts); err == nil {
		t.Fatal("a failed bairro read must fail the section, never answer a partial count")
	}
}

func TestExtentTopCityAndPeekStayInsideTheFilter(t *testing.T) {
	m := compiledMap(t)
	cases := map[string]func() (string, []interface{}){
		"extent":   m.extent,
		"top city": m.topCity,
		"peek": func() (string, []interface{}) {
			sql, args, err := m.leadsAt(geo.Point{Lat: -23.5614, Lng: -46.6559}, crmfilter.PlacementOnMap, leadmap.MaxPeekLeads)
			if err != nil {
				t.Fatal(err)
			}
			return sql, args
		},
	}
	for name, build := range cases {
		sql, args := build()
		assertPlaceholders(t, name, sql, args)
		if !strings.Contains(sql, ".workspace_id = ? AND ") || !strings.Contains(sql, ".is_primary") || !strings.Contains(sql, ".lead_id IN (SELECT leads.id FROM leads WHERE "+m.leads.where+")") {
			t.Fatalf("%s leaves the filter or the workspace:\n%s", name, sql)
		}
	}
	sql, args, err := m.leadsAt(geo.Point{Lat: -23.5614, Lng: -46.6559}, crmfilter.PlacementOnMap, leadmap.MaxPeekLeads)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "(a.latitude IS NOT NULL AND a.geo_precision = ANY(?)) AND a.latitude = ? AND a.longitude = ?") ||
		!strings.Contains(sql, "COUNT(*) OVER () AS total") || !strings.HasSuffix(sql, "ORDER BY a.lead_id LIMIT ?") {
		t.Fatalf("peek sql = %s", sql)
	}
	if args[2] != -23.5614 || args[3] != -46.6559 {
		t.Fatalf("the peek binds latitude then longitude, got %#v", args[:4])
	}
}

func TestThePeekOfAnApproximatePointListsOnlyTheApproximateLeadsThere(t *testing.T) {
	m := compiledMap(t)
	sql, args, err := m.leadsAt(geo.Point{Lat: -6.3104, Lng: -35.4793}, crmfilter.PlacementApproximate, leadmap.MaxPeekLeads)
	if err != nil {
		t.Fatal(err)
	}
	assertPlaceholders(t, "approximate peek", sql, args)
	if !strings.Contains(sql, "(a.latitude IS NOT NULL AND NOT COALESCE(a.geo_precision = ANY(?), false)) AND a.latitude = ? AND a.longitude = ?") {
		t.Fatalf("approximate peek sql = %s", sql)
	}
	if _, _, err := m.leadsAt(geo.Point{Lat: -6.3104, Lng: -35.4793}, crmfilter.PlacementPending, leadmap.MaxPeekLeads); !errors.Is(err, leadmap.ErrInvalidPlacement) {
		t.Fatalf("a point is never pending, got %v", err)
	}
}

func expectMapSession(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL work_mem = '32MB'")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL jit = off")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL statement_timeout = '5s'")).WillReturnResult(sqlmock.NewResult(0, 0))
}

func TestMapReaderRunsTheSummaryAndScansEveryBucket(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectMapSession(mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) AS total,")).
		WillReturnRows(sqlmock.NewRows([]string{"total", "with_address", "on_map", "approximate", "not_found", "pending", "quota_exceeded", "refused"}).
			AddRow(100, 75, 40, 20, 5, 8, 2, 3))
	mock.ExpectCommit()
	got, err := NewMapReader(db).Summary(context.Background(), leadmap.Scope{WorkspaceID: mapWorkspace, Filter: blockedFilter()})
	if err != nil {
		t.Fatal(err)
	}
	want := leadmap.Summary{Total: 100, OnMap: 40, Approximate: 20, WithoutAddress: 25, NotFound: 5, Pending: 8, QuotaExceeded: 2, Refused: 3}
	if got != want {
		t.Fatalf("summary = %+v, want %+v", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMapReaderAnswersCellsPastTheSwitchAndReadsPointsOnlyUnderIt(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	positions := func(n int) *sqlmock.Rows { return sqlmock.NewRows([]string{"positions"}).AddRow(n) }
	cellColumns := []string{"placement", "ix", "iy", "people", "lat", "lng"}
	pointColumns := []string{"lat", "lng", "placement", "people", "precision_rank", "lead_ids", "value"}
	positionsSQL := regexp.QuoteMeta("SELECT count(*) AS positions FROM (")
	cellsSQL := regexp.QuoteMeta("SELECT CASE WHEN")
	pointsSQL := regexp.QuoteMeta("SELECT a.latitude AS lat, a.longitude AS lng")
	q := layerRequest(t, "classificacao")
	q.MaxPoints = 2

	expectMapSession(mock)
	mock.ExpectQuery(positionsSQL).WillReturnRows(positions(3))
	mock.ExpectQuery(cellsSQL).
		WillReturnRows(sqlmock.NewRows(cellColumns).
			AddRow("on_map", -4246, -2143, 9000, -23.55, -46.65).
			AddRow("approximate", -4246, -2143, 40, -23.56, -46.66))
	mock.ExpectCommit()

	expectMapSession(mock)
	mock.ExpectQuery(positionsSQL).WillReturnRows(positions(2))
	mock.ExpectQuery(pointsSQL).
		WillReturnRows(sqlmock.NewRows(pointColumns).
			AddRow(-23.5614, -46.6559, "on_map", 7, 3, "{a,b,c,d,e}", "Positivo").
			AddRow(-23.5614, -46.6559, "approximate", 2, 5, "{f,g}", nil))
	mock.ExpectCommit()

	expectMapSession(mock)
	mock.ExpectQuery(positionsSQL).WillReturnRows(positions(0))
	mock.ExpectCommit()

	expectMapSession(mock)
	mock.ExpectQuery(positionsSQL).WillReturnRows(positions(9))
	mock.ExpectQuery(cellsSQL).WillReturnRows(sqlmock.NewRows(cellColumns).AddRow("pending", -4246, -2143, 1, -23.5614, -46.6559))
	mock.ExpectRollback()

	expectMapSession(mock)
	mock.ExpectQuery(positionsSQL).WillReturnRows(positions(1))
	mock.ExpectQuery(pointsSQL).WillReturnRows(sqlmock.NewRows(pointColumns).AddRow(-23.5614, -46.6559, "pending", 1, 5, "{h}", nil))
	mock.ExpectRollback()

	reader := NewMapReader(db)
	scope := leadmap.Scope{WorkspaceID: mapWorkspace}
	cells, err := reader.Layer(context.Background(), scope, q)
	if err != nil {
		t.Fatal(err)
	}
	if cells.Kind != leadmap.LayerCells || cells.CellSize != q.CellSize || len(cells.Cells) != 2 || cells.Cells[0].IX != -4246 || cells.Cells[0].People != 9000 ||
		cells.Cells[0].Placement != crmfilter.PlacementOnMap || cells.Cells[1].Placement != crmfilter.PlacementApproximate || cells.Cells[1].People != 40 {
		t.Fatalf("cells = %+v", cells)
	}
	points, err := reader.Layer(context.Background(), scope, q)
	if err != nil {
		t.Fatal(err)
	}
	if points.Kind != leadmap.LayerPoints || len(points.Points) != 2 {
		t.Fatalf("points = %+v", points)
	}
	p := points.Points[0]
	if p.People != 7 || len(p.LeadIDs) != 5 || p.Precision != geo.PrecisionStreet || p.Value != "Positivo" || p.Placement != crmfilter.PlacementOnMap {
		t.Fatalf("point = %+v", p)
	}
	if near := points.Points[1]; near.Placement != crmfilter.PlacementApproximate || near.Precision != geo.PrecisionDistrict || near.People != 2 || near.ID() == p.ID() {
		t.Fatalf("approximate point = %+v", near)
	}
	empty, err := reader.Layer(context.Background(), scope, q)
	if err != nil || empty.Kind != leadmap.LayerPoints || len(empty.Points) != 0 || empty.Points == nil {
		t.Fatalf("an empty tile = %+v %v, want no points", empty, err)
	}
	if _, err := reader.Layer(context.Background(), scope, q); err == nil {
		t.Fatal("a cell in neither placement must fail the layer, never be drawn as a guess")
	}
	if _, err := reader.Layer(context.Background(), scope, q); err == nil {
		t.Fatal("a point in neither placement must fail the layer, never be drawn as a guess")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMapReaderRefusesAnUnboundAreaAsAnInvalidFilter(t *testing.T) {
	db, _, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldArea, Operator: crmfilter.OpIn, Values: []string{"11111111-1111-4111-8111-111111111111"}},
	}}}}
	if _, err := NewMapReader(db).Summary(context.Background(), leadmap.Scope{WorkspaceID: mapWorkspace, Filter: f}); err == nil || !strings.Contains(err.Error(), "invalid filter") {
		t.Fatalf("an area the use case did not resolve = %v, want an invalid filter and no query", err)
	}
}

func expectAreaSession(mock sqlmock.Sqlmock) {
	expectMapSession(mock)
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL enable_nestloop = off")).WillReturnResult(sqlmock.NewResult(0, 0))
}

func TestAnAreaFilterRunsWithHashJoinsSoAnUnderestimatedAreaCannotPickNestedLoops(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectAreaSession(mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) AS total,")).
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(3))
	mock.ExpectCommit()
	if _, err := NewMapReader(db).Summary(context.Background(), leadmap.Scope{WorkspaceID: mapWorkspace, Filter: boundAreaFilter()}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func boundAreaFilter() crmfilter.Filter {
	return boundAreaFilterEditedAt(time.Time{})
}

func boundAreaFilterEditedAt(edited time.Time) crmfilter.Filter {
	id := "11111111-1111-4111-8111-111111111111"
	return crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{
		crmfilter.Predicate{Field: crmfilter.FieldArea, Operator: crmfilter.OpIn, Values: []string{id}}.BindAreas([]crmfilter.AreaBounds{{ID: id, South: -23.6, West: -46.7, North: -23.5, East: -46.6, UpdatedAt: edited}}),
	}}}}
}

func TestTheLeadListCountsAndPagesAnAreaInTheAreaSession(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectAreaSession(mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM leads WHERE ")).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectCommit()
	expectAreaSession(mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT leads.id FROM leads WHERE ")).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("22222222-2222-4222-8222-222222222222"))
	mock.ExpectCommit()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT leads.id, leads.workspace_id")).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	input := lead.ListLeadsInput{WorkspaceID: mapWorkspace, Filter: boundAreaFilter(), Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 20}}}
	if _, err := repo.ListWithSummary(input); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheLeadListWithoutAnAreaKeepsItsPlainReads(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM leads WHERE ")).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT leads.id FROM leads WHERE ")).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	input := lead.ListLeadsInput{WorkspaceID: mapWorkspace, Filter: blockedFilter(), Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 20}}}
	if _, err := (&repository{db: db, agg: newAggregateCache(nil)}).ListWithSummary(input); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func expectAreaFacets(mock sqlmock.Sqlmock, total int64) {
	expectAreaSession(mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) AS total, COUNT(*) FILTER (WHERE leads.blocked) AS blocked,")).
		WillReturnRows(sqlmock.NewRows([]string{"total", "blocked", "window_open", "with_campaign", "with_memory", "named"}).AddRow(total, 0, 0, 0, 0, 0))
	mock.ExpectCommit()
	expectAreaSession(mock)
	for range 3 {
		mock.ExpectQuery(regexp.QuoteMeta("SELECT s.key AS key, COUNT(*) AS count FROM (SELECT DISTINCT ")).WillReturnRows(counts())
	}
	mock.ExpectCommit()
}

func expectAreaCountAndIDs(mock sqlmock.Sqlmock, total int64) {
	expectAreaSession(mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM leads WHERE ")).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(total))
	mock.ExpectCommit()
	expectAreaSession(mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT leads.id FROM leads WHERE ")).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectCommit()
}

func TestTheCachedFacetsReadAgainWhenAnAreaIsReshapedInsideTheSameBounds(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := &repository{db: db, agg: newAggregateCache(newFakeState())}
	drawn := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	reshaped := drawn.Add(time.Second)
	expectAreaFacets(mock, 7)
	expectAreaFacets(mock, 3)
	for _, step := range []struct {
		edited time.Time
		want   int64
	}{{drawn, 7}, {drawn, 7}, {reshaped, 3}} {
		facets, err := repo.Facets(lead.ListLeadsInput{WorkspaceID: mapWorkspace, Filter: boundAreaFilterEditedAt(step.edited)})
		if err != nil {
			t.Fatal(err)
		}
		if facets.Total != step.want {
			t.Fatalf("facets total for the area edited at %v = %d, want %d", step.edited, facets.Total, step.want)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheCachedListTotalReadsAgainWhenAnAreaIsReshapedInsideTheSameBounds(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := &repository{db: db, agg: newAggregateCache(newFakeState())}
	drawn := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	expectAreaCountAndIDs(mock, 7)
	expectAreaCountAndIDs(mock, 3)
	for _, step := range []struct {
		edited time.Time
		want   int64
	}{{drawn, 7}, {drawn.Add(time.Second), 3}} {
		input := lead.ListLeadsInput{WorkspaceID: mapWorkspace, Filter: boundAreaFilterEditedAt(step.edited), Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 20}}}
		page, err := repo.ListWithSummary(input)
		if err != nil {
			t.Fatal(err)
		}
		if page.TotalItems != step.want {
			t.Fatalf("total for the area edited at %v = %d, want %d", step.edited, page.TotalItems, step.want)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func boundApproximateAreaFilter() crmfilter.Filter {
	f := boundAreaFilter()
	f.Groups[0].Predicates[0].Field = crmfilter.FieldAreaApproximate
	return f
}

func TestAnApproximateAreaFilterRunsInTheAreaSessionToo(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectAreaSession(mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) AS total,")).
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(3))
	mock.ExpectCommit()
	if _, err := NewMapReader(db).Summary(context.Background(), leadmap.Scope{WorkspaceID: mapWorkspace, Filter: boundApproximateAreaFilter()}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
