package lead

import (
	"vozko/domain/crmfilter"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

const summaryAddressAlias = "la"

func placementCountsSQL(placements ...crmfilter.GeoPlacement) (string, []interface{}) {
	return infracrmfilter.GeoPlacementCountsSQL(summaryAddressAlias, placements...)
}
