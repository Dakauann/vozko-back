package calllist_repository

import "vozko/domain/calls/callhistory"

func callAttemptSQL(alias string) string {
	return alias + ".direction = '" + string(callhistory.AttemptDirection) + "' AND " + alias + ".deleted_at IS NULL"
}
