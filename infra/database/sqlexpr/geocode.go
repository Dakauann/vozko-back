package sqlexpr

import "vozko/domain/lead"

func GeocodeQueued(alias string) string {
	return column(alias, "geo_status") + " IN (" + LiteralList(stringsOf(lead.QueuedGeoStatuses())) + ")"
}
