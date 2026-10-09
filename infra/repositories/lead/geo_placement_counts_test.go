package lead

import (
	"reflect"
	"testing"

	"vozko/domain/crmfilter"
)

func TestPlacementCountsNameEachBucketAfterItsPlacement(t *testing.T) {
	sql, args := placementCountsSQL(crmfilter.PlacementOnMap, crmfilter.PlacementWithoutAddress)
	want := ", COUNT(*) FILTER (WHERE la.latitude IS NOT NULL AND la.geo_precision = ANY(?)) AS on_map" +
		", COUNT(*) FILTER (WHERE la.lead_id IS NULL) AS without_address"
	if sql != want || !reflect.DeepEqual(args, []interface{}{pinning}) {
		t.Fatalf("counts = %q %#v\nwant     %q", sql, args, want)
	}
}

func TestPlacementCountsRefuseAnUnknownPlacementInsteadOfCountingNothing(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an unknown placement must stop the summary from being built")
		}
	}()
	placementCountsSQL(crmfilter.GeoPlacement("somewhere"))
}
