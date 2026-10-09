package database

import (
	"strings"
	"testing"
)

func TestPlaceSearchPrefixIndexesUsePatternOperators(t *testing.T) {
	prefixes := map[string]string{
		GeoCityPrefixIndex:          "ON geo_cities (name_key text_pattern_ops)",
		GeoDistrictCityPrefixIndex:  "ON geo_district_points (city_code, district_key text_pattern_ops)",
		GeoDistrictPrefixIndex:      "ON geo_district_points (district_key text_pattern_ops)",
		GeoCEPPrefixIndex:           "ON geo_cep_points (zip_code text_pattern_ops)",
		GeoCEPStreetCityPrefixIndex: "ON geo_cep_streets (city_code, street_key text_pattern_ops)",
		GeoCEPStreetPrefixIndex:     "ON geo_cep_streets (street_key text_pattern_ops)",
		GeoCEPStreetStateBuiltIndex: "ON geo_cep_streets (state, built_at)",
	}
	if got := len(PlaceSearchIndexNames()); got != len(prefixes) {
		t.Fatalf("place search builds %d indexes, want %d", got, len(prefixes))
	}
	for name, on := range prefixes {
		sql, ok := ConcurrentIndexSQL(name)
		if !ok {
			t.Fatalf("%s is not built at boot", name)
		}
		if flat := strings.Join(strings.Fields(sql), " "); !strings.HasSuffix(flat, on) {
			t.Fatalf("%s = %q, want it %s", name, flat, on)
		}
	}
}
