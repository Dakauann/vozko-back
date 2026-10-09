package database

import "vozko/domain/calls/callhistory"

func CallAttemptSQL(alias string) string {
	return alias + ".direction = '" + string(callhistory.AttemptDirection) + "' AND " + alias + ".deleted_at IS NULL"
}
