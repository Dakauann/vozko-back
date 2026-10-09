package attendance_repository

import "vozko/infra/database"

func realMessageSQL(alias string) string {
	return database.RealMessageSQL(alias)
}

func liveMessageSQL(alias string) string {
	return database.LiveMessageSQL(alias)
}
