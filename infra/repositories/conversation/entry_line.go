package conversation_repository

import "strings"

func EntryLineSQL(entryTypeColumn, entryIDColumn string) string {
	var b strings.Builder
	b.WriteString("COALESCE(CASE " + entryTypeColumn)
	for _, q := range channelQueries {
		b.WriteString(" WHEN '" + string(q.EntryType) + "' THEN (SELECT " + q.LineField +
			" FROM (SELECT 1) AS line_anchor " + q.entryJoinOn(entryIDColumn) + " LIMIT 1)")
	}
	b.WriteString(" END, '')")
	return b.String()
}
