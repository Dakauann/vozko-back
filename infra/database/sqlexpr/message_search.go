package sqlexpr

func MessageSearchText(alias string) string {
	return "LOWER(COALESCE(" + column(alias, "text") + ", '') || ' ' || COALESCE(" + column(alias, "transcript") + ", ''))"
}

func SearchableMessage(alias string) string {
	return column(alias, "deleted_at") + " IS NULL AND " + column(alias, "retracted_at") + " IS NULL"
}

func MessageTextMatches(alias string) string {
	return SearchableMessage(alias) + " AND " + MessageSearchText(alias) + " LIKE LOWER(?)"
}
