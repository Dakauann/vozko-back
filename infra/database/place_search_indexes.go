package database

const (
	GeoCityPrefixIndex          = "idx_geo_cities_name_prefix"
	GeoDistrictCityPrefixIndex  = "idx_geo_district_points_city_prefix"
	GeoDistrictPrefixIndex      = "idx_geo_district_points_prefix"
	GeoCEPPrefixIndex           = "idx_geo_cep_points_zip_prefix"
	GeoCEPStreetCityPrefixIndex = "idx_geo_cep_streets_city_prefix"
	GeoCEPStreetPrefixIndex     = "idx_geo_cep_streets_prefix"
	GeoCEPStreetStateBuiltIndex = "idx_geo_cep_streets_state_built"
)

func placeSearchIndexes() []concurrentIndex {
	return []concurrentIndex{
		{
			name: GeoCityPrefixIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + GeoCityPrefixIndex + `
				ON geo_cities (name_key text_pattern_ops)`,
		},
		{
			name: GeoDistrictCityPrefixIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + GeoDistrictCityPrefixIndex + `
				ON geo_district_points (city_code, district_key text_pattern_ops)`,
		},
		{
			name: GeoDistrictPrefixIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + GeoDistrictPrefixIndex + `
				ON geo_district_points (district_key text_pattern_ops)`,
		},
		{
			name: GeoCEPPrefixIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + GeoCEPPrefixIndex + `
				ON geo_cep_points (zip_code text_pattern_ops)`,
		},
		{
			name: GeoCEPStreetCityPrefixIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + GeoCEPStreetCityPrefixIndex + `
				ON geo_cep_streets (city_code, street_key text_pattern_ops)`,
		},
		{
			name: GeoCEPStreetPrefixIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + GeoCEPStreetPrefixIndex + `
				ON geo_cep_streets (street_key text_pattern_ops)`,
		},
		{
			name: GeoCEPStreetStateBuiltIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + GeoCEPStreetStateBuiltIndex + `
				ON geo_cep_streets (state, built_at)`,
		},
	}
}

func PlaceSearchIndexNames() []string {
	indexes := placeSearchIndexes()
	names := make([]string, len(indexes))
	for i, idx := range indexes {
		names[i] = idx.name
	}
	return names
}
