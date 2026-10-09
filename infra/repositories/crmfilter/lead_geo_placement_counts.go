package crmfilter

import (
	"strings"

	"vozko/domain/crmfilter"
)

func GeoPlacementCountsSQL(alias string, placements ...crmfilter.GeoPlacement) (string, []interface{}) {
	var b strings.Builder
	var args []interface{}
	for _, p := range placements {
		condition, conditionArgs, err := GeoPlacementCondition(alias, p)
		if err != nil {
			panic(err)
		}
		b.WriteString(", COUNT(*) FILTER (WHERE " + condition + ") AS " + string(p))
		args = append(args, conditionArgs...)
	}
	return b.String(), args
}
