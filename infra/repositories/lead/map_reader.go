package lead

import (
	"context"
	"fmt"
	"strconv"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
	"vozko/domain/leadmap"
	"vozko/infra/database"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

type mapReader struct {
	db    *gorm.DB
	leads *repository
}

func NewMapReader(db *gorm.DB) leadmap.Reader {
	return &mapReader{db: db, leads: &repository{db: db, agg: newAggregateCache(nil)}}
}

const mapStatementTimeout = "5s"

var (
	mapSessionSettings  = database.ReadSessionSettings("32MB", mapStatementTimeout)
	areaSessionSettings = []string{"SET LOCAL enable_nestloop = off"}
	inReadSession       = database.InReadSession
)

type mapQuery struct {
	leads *listQuery
}

func (m mapQuery) settings() []string {
	return m.leads.sessionSettings(mapSessionSettings)
}

func (m mapQuery) workspaceID() string {
	return m.leads.desc.WorkspaceID
}

func (m mapQuery) members() string {
	return "a.lead_id IN (" + m.leads.filteredIDs() + ")"
}

func (r *mapReader) scope(s leadmap.Scope) (mapQuery, error) {
	q, err := r.leads.compile(s.ListInput())
	if err != nil {
		return mapQuery{}, err
	}
	return mapQuery{leads: q}, nil
}

func (q *listQuery) sessionSettings(base []string) []string {
	if !q.areas {
		return base
	}
	return append(append([]string(nil), base...), areaSessionSettings...)
}

func (r *mapReader) scan(ctx context.Context, m mapQuery, query string, args []interface{}, into interface{}) error {
	return inReadSession(ctx, r.db, m.settings(), func(tx *gorm.DB) error {
		return tx.Raw(query, args...).Scan(into).Error
	})
}

func precisionOfRank(rank *int) geo.Precision {
	ranks := geo.PrecisionsBestFirst()
	if rank == nil || *rank < 1 || *rank > len(ranks) {
		return ""
	}
	return ranks[*rank-1]
}

func (m mapQuery) locatedIn(c geo.Claim) (string, []interface{}) {
	where := "a.workspace_id = ? AND a.is_primary AND a.latitude IS NOT NULL" +
		" AND a.latitude > ? AND a.latitude <= ? AND a.longitude >= ? AND a.longitude < ?" +
		" AND " + m.members()
	args := append([]interface{}{m.workspaceID(), c.South, c.North, c.West, c.East}, m.leads.args...)
	return where, args
}

func placementClass(alias string) (string, []interface{}) {
	onMap, args, err := infracrmfilter.GeoPlacementCondition(alias, crmfilter.PlacementOnMap)
	if err != nil {
		panic(err)
	}
	return "CASE WHEN " + onMap + " THEN '" + string(crmfilter.PlacementOnMap) + "' ELSE '" + string(crmfilter.PlacementApproximate) + "' END AS placement", args
}

func (m mapQuery) layerPositions(q leadmap.LayerRequest) (string, []interface{}) {
	where, whereArgs := m.locatedIn(q.Claim)
	class, classArgs := placementClass("a")
	sql := "SELECT count(*) AS positions FROM (SELECT a.latitude, a.longitude, " + class +
		" FROM lead_addresses a WHERE " + where + " GROUP BY 1, 2, 3 LIMIT ?) d"
	return sql, append(append(append([]interface{}{}, classArgs...), whereArgs...), q.MaxPoints+1)
}

func (m mapQuery) layerCells(q leadmap.LayerRequest) (string, []interface{}) {
	where, whereArgs := m.locatedIn(q.Claim)
	class, classArgs := placementClass("a")
	sql := "SELECT " + class + ", floor(a.longitude / ?::float8)::bigint AS ix, floor(a.latitude / ?::float8)::bigint AS iy," +
		" COUNT(*) AS people, avg(a.latitude) AS lat, avg(a.longitude) AS lng" +
		" FROM lead_addresses a WHERE " + where + " GROUP BY 1, 2, 3"
	return sql, append(append(append([]interface{}{}, classArgs...), q.CellSize, q.CellSize), whereArgs...)
}

func (m mapQuery) layerPoints(q leadmap.LayerRequest) (string, []interface{}) {
	where, whereArgs := m.locatedIn(q.Claim)
	class, classArgs := placementClass("a")
	args := append(append([]interface{}{}, classArgs...), infracrmfilter.PrecisionArray(geo.PrecisionsBestFirst()))
	value := "NULL::text AS value"
	if q.ColorByKey != "" {
		value = "mode() WITHIN GROUP (ORDER BY (SELECT lc.custom_fields ->> ?::text FROM leads lc WHERE lc.id = a.lead_id AND lc.workspace_id = a.workspace_id)) AS value"
		args = append(args, q.ColorByKey)
	}
	sql := "SELECT a.latitude AS lat, a.longitude AS lng, " + class + ", COUNT(*) AS people," +
		" min(array_position(?::text[], a.geo_precision::text)) AS precision_rank," +
		" (array_agg(a.lead_id::text))[1:" + strconv.Itoa(leadmap.MaxLeadIDsPerPoint) + "] AS lead_ids," +
		" " + value +
		" FROM lead_addresses a WHERE " + where +
		" GROUP BY 1, 2, 3"
	return sql, append(args, whereArgs...)
}

func (m mapQuery) summary() (string, []interface{}) {
	extra, args := geoStatusCountsSQL()
	return m.leads.addressSummarySQL(extra, args)
}

func referenceCityOf(alias, cityKey string) string {
	return alias + ".state = upper(split_part(" + cityKey + ", ':', 1)) AND " + alias + ".name_key = substr(" + cityKey + ", strpos(" + cityKey + ", ':') + 1)"
}

func referenceCityCode(code, cityKey string) string {
	return "COALESCE(" + code + ", (SELECT gc.city_code FROM geo_cities gc WHERE " + referenceCityOf("gc", cityKey) + " ORDER BY gc.city_code LIMIT 1))"
}

const (
	addressCityCode = ", max(la.city_code) AS city_code"
	groupCityCode   = ", max(g.city_code) AS city_code"
)

func (m mapQuery) districts(limit int) (string, []interface{}) {
	placing := infracrmfilter.PrecisionArray(leadmap.DistrictPointPrecisions())
	placed := "la.latitude IS NOT NULL AND la.geo_precision = ANY(?)"
	inner := ", COUNT(*) FILTER (WHERE " + placed + ") AS placed, sum(la.latitude) FILTER (WHERE " + placed + ") AS lat_sum, sum(la.longitude) FILTER (WHERE " + placed + ") AS lng_sum" + addressCityCode
	code := referenceCityCode("max(g.city_code)", "g.city_key")
	outer := ", sum(g.lat_sum) / NULLIF(sum(g.placed), 0) AS lat, sum(g.lng_sum) / NULLIF(sum(g.placed), 0) AS lng, " + code + " AS city_code"
	having := " HAVING sum(g.placed) > 0 OR EXISTS (SELECT 1 FROM geo_district_points dpx WHERE dpx.city_code = " + code + " AND dpx.district_key = g.district_key)"
	counted, args := m.leads.districtCountsSQL(inner, []interface{}{placing, placing, placing}, outer, having, limit)
	sql := "SELECT d.city_key, d.district_key, d.district, d.count, COALESCE(dp.latitude, d.lat) AS lat, COALESCE(dp.longitude, d.lng) AS lng" +
		" FROM (" + counted + ") d LEFT JOIN geo_district_points dp ON dp.city_code = d.city_code AND dp.district_key = d.district_key" +
		" ORDER BY d.count DESC, d.city_key, d.district_key"
	return sql, args
}

func (m mapQuery) leftOutTotal() (string, []interface{}) {
	return "SELECT COUNT(*) AS total FROM leads WHERE " + m.leads.where, append([]interface{}{}, m.leads.args...)
}

func (m mapQuery) leftOutDistricts(limit int) (string, []interface{}) {
	return m.leads.districtCountsSQL("", nil, "", "", limit)
}

func (m mapQuery) extent() (string, []interface{}) {
	sql := "SELECT min(a.latitude) AS south, min(a.longitude) AS west, max(a.latitude) AS north, max(a.longitude) AS east" +
		" FROM lead_addresses a WHERE a.workspace_id = ? AND a.is_primary AND a.latitude IS NOT NULL AND " + m.members()
	return sql, append([]interface{}{m.workspaceID()}, m.leads.args...)
}

func (m mapQuery) topCity() (string, []interface{}) {
	cities, args := m.leads.cityCountsSQL(addressCityCode, groupCityCode, 1)
	sql := "SELECT t.city_key, t.city, t.state, t.count, gcr.latitude AS lat, gcr.longitude AS lng FROM (" + cities + ") t" +
		" LEFT JOIN geo_cities gcr ON gcr.city_code = " + referenceCityCode("t.city_code", "t.city_key")
	return sql, args
}

func (m mapQuery) leadsAt(at geo.Point, placement crmfilter.GeoPlacement, limit int) (string, []interface{}, error) {
	if placement != crmfilter.PlacementOnMap && placement != crmfilter.PlacementApproximate {
		return "", nil, fmt.Errorf("%w: %q", leadmap.ErrInvalidPlacement, placement)
	}
	condition, conditionArgs, err := infracrmfilter.GeoPlacementCondition("a", placement)
	if err != nil {
		return "", nil, err
	}
	sql := "SELECT a.lead_id::text AS lead_id, COUNT(*) OVER () AS total FROM lead_addresses a" +
		" WHERE a.workspace_id = ? AND a.is_primary AND (" + condition + ")" +
		" AND a.latitude = ? AND a.longitude = ?" +
		" AND " + m.members() +
		" ORDER BY a.lead_id LIMIT ?"
	args := append(append([]interface{}{m.workspaceID()}, conditionArgs...), at.Lat, at.Lng)
	args = append(args, m.leads.args...)
	return sql, append(args, limit), nil
}

func (r *mapReader) Summary(ctx context.Context, s leadmap.Scope) (leadmap.Summary, error) {
	m, err := r.scope(s)
	if err != nil {
		return leadmap.Summary{}, err
	}
	sql, args := m.summary()
	var row infracrmfilter.LeadAddressSummaryRow
	if err := r.scan(ctx, m, sql, args, &row); err != nil {
		return leadmap.Summary{}, err
	}
	return row.Summary(), nil
}

type cellRow struct {
	Placement string
	IX, IY    int64
	People    int
	Lat, Lng  float64
}

type pointRow struct {
	Lat, Lng      float64
	Placement     string
	People        int
	PrecisionRank *int
	LeadIDs       pq.StringArray `gorm:"column:lead_ids;type:text[]"`
	Value         *string
}

func (r *mapReader) Layer(ctx context.Context, s leadmap.Scope, q leadmap.LayerRequest) (leadmap.Layer, error) {
	m, err := r.scope(s)
	if err != nil {
		return leadmap.Layer{}, err
	}
	var layer leadmap.Layer
	err = inReadSession(ctx, r.db, m.settings(), func(tx *gorm.DB) error {
		positionsSQL, positionsArgs := m.layerPositions(q)
		var counted struct{ Positions int64 }
		if err := tx.Raw(positionsSQL, positionsArgs...).Scan(&counted).Error; err != nil {
			return fmt.Errorf("lead map positions: %w", err)
		}
		if counted.Positions == 0 {
			layer = leadmap.Layer{Kind: leadmap.LayerPoints, Points: []leadmap.Point{}}
			return nil
		}
		if counted.Positions > int64(q.MaxPoints) {
			cellsSQL, cellsArgs := m.layerCells(q)
			var cells []cellRow
			if err := tx.Raw(cellsSQL, cellsArgs...).Scan(&cells).Error; err != nil {
				return fmt.Errorf("lead map cells: %w", err)
			}
			layer, err = cellLayer(q.CellSize, cells)
			return err
		}
		pointsSQL, pointsArgs := m.layerPoints(q)
		var points []pointRow
		if err := tx.Raw(pointsSQL, pointsArgs...).Scan(&points).Error; err != nil {
			return fmt.Errorf("lead map points: %w", err)
		}
		layer, err = pointLayer(points)
		return err
	})
	if err != nil {
		return leadmap.Layer{}, err
	}
	return layer, nil
}

func cellLayer(size float64, rows []cellRow) (leadmap.Layer, error) {
	cells := make([]leadmap.Cell, 0, len(rows))
	for _, row := range rows {
		placement, err := layerPlacement(row.Placement)
		if err != nil {
			return leadmap.Layer{}, err
		}
		cells = append(cells, leadmap.Cell{IX: row.IX, IY: row.IY, Placement: placement, People: row.People, Center: geo.Point{Lat: row.Lat, Lng: row.Lng}})
	}
	return leadmap.Layer{Kind: leadmap.LayerCells, CellSize: size, Cells: cells}, nil
}

func pointLayer(rows []pointRow) (leadmap.Layer, error) {
	points := make([]leadmap.Point, 0, len(rows))
	for _, row := range rows {
		value := ""
		if row.Value != nil {
			value = *row.Value
		}
		placement, err := layerPlacement(row.Placement)
		if err != nil {
			return leadmap.Layer{}, err
		}
		points = append(points, leadmap.NewPoint(geo.Point{Lat: row.Lat, Lng: row.Lng}, precisionOfRank(row.PrecisionRank), placement, row.People, row.LeadIDs, value))
	}
	return leadmap.Layer{Kind: leadmap.LayerPoints, Points: points}, nil
}

func (r *mapReader) Districts(ctx context.Context, s leadmap.Scope, limit int) ([]leadmap.District, error) {
	m, err := r.scope(s)
	if err != nil {
		return nil, err
	}
	sql, args := m.districts(limit)
	var rows []struct {
		CityKey, DistrictKey, District string
		Count                          int
		Lat, Lng                       *float64
	}
	if err := r.scan(ctx, m, sql, args, &rows); err != nil {
		return nil, err
	}
	out := make([]leadmap.District, 0, len(rows))
	for _, row := range rows {
		d := leadmap.District{CityKey: row.CityKey, DistrictKey: row.DistrictKey, Name: row.District, People: row.Count}
		if row.Lat != nil && row.Lng != nil {
			d.Position = geo.Point{Lat: *row.Lat, Lng: *row.Lng}
		}
		out = append(out, d)
	}
	return out, nil
}

func (r *mapReader) Extent(ctx context.Context, s leadmap.Scope) (*geo.BBox, error) {
	m, err := r.scope(s)
	if err != nil {
		return nil, err
	}
	sql, args := m.extent()
	var row struct{ South, West, North, East *float64 }
	if err := r.scan(ctx, m, sql, args, &row); err != nil {
		return nil, err
	}
	if row.South == nil || row.West == nil || row.North == nil || row.East == nil {
		return nil, nil
	}
	return &geo.BBox{South: *row.South, West: *row.West, North: *row.North, East: *row.East}, nil
}

func (r *mapReader) TopCity(ctx context.Context, s leadmap.Scope) (*leadmap.CityPlace, error) {
	m, err := r.scope(s)
	if err != nil {
		return nil, err
	}
	sql, args := m.topCity()
	var rows []struct {
		CityKey, City, State string
		Count                int64
		Lat, Lng             *float64
	}
	if err := r.scan(ctx, m, sql, args, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	top := rows[0]
	place := &leadmap.CityPlace{CityKey: top.CityKey, Name: top.City, State: top.State, People: int(top.Count)}
	if top.Lat != nil && top.Lng != nil {
		place.Center = &geo.Point{Lat: *top.Lat, Lng: *top.Lng}
	}
	return place, nil
}

func layerPlacement(raw string) (crmfilter.GeoPlacement, error) {
	switch p := crmfilter.GeoPlacement(raw); p {
	case crmfilter.PlacementOnMap, crmfilter.PlacementApproximate:
		return p, nil
	}
	return "", fmt.Errorf("lead map layer: %q is neither on the map nor approximate", raw)
}

func (r *mapReader) LeadsAt(ctx context.Context, s leadmap.Scope, at geo.Point, placement crmfilter.GeoPlacement, limit int) ([]string, int, error) {
	m, err := r.scope(s)
	if err != nil {
		return nil, 0, err
	}
	sql, args, err := m.leadsAt(at, placement, limit)
	if err != nil {
		return nil, 0, err
	}
	var rows []struct {
		LeadID string
		Total  int
	}
	if err := r.scan(ctx, m, sql, args, &rows); err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(rows))
	total := 0
	for _, row := range rows {
		ids = append(ids, row.LeadID)
		total = row.Total
	}
	return ids, total, nil
}

func (r *mapReader) LeftOut(ctx context.Context, s leadmap.Scope, limit int) (leadmap.LeftOut, error) {
	m, err := r.scope(s)
	if err != nil {
		return leadmap.LeftOut{}, err
	}
	out := leadmap.NoneLeftOut()
	err = inReadSession(ctx, r.db, m.settings(), func(tx *gorm.DB) error {
		totalSQL, totalArgs := m.leftOutTotal()
		var counted struct{ Total int64 }
		if err := tx.Raw(totalSQL, totalArgs...).Scan(&counted).Error; err != nil {
			return fmt.Errorf("left out leads: %w", err)
		}
		districtSQL, districtArgs := m.leftOutDistricts(limit)
		if err := tx.Raw(districtSQL, districtArgs...).Scan(&out.Districts).Error; err != nil {
			return fmt.Errorf("left out bairros: %w", err)
		}
		out.Total = counted.Total
		return nil
	})
	if err != nil {
		return leadmap.LeftOut{}, err
	}
	for i := range out.Districts {
		out.Districts[i].Pair = crmfilter.DistrictPair(out.Districts[i].CityKey, out.Districts[i].DistrictKey)
	}
	return out, nil
}
