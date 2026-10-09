package database

import (
	"strings"

	"vozko/domain/lead"
)

const GeocodeQueueIndex = "idx_lead_addresses_geo_queue_ws"

func GeocodeQueuedSQL(alias string) string {
	column := "geo_status"
	if alias != "" {
		column = alias + ".geo_status"
	}
	statuses := lead.QueuedGeoStatuses()
	quoted := make([]string, len(statuses))
	for i, status := range statuses {
		quoted[i] = "'" + string(status) + "'"
	}
	return column + " IN (" + strings.Join(quoted, ", ") + ")"
}
