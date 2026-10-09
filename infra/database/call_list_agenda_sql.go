package database

const CallListAgendaIndex = "idx_call_list_items_agenda"

func CallListAgendaSQL(alias string) string {
	column := "callback_at"
	if alias != "" {
		column = alias + "." + column
	}
	return "COALESCE(" + column + ", 'infinity'::timestamptz)"
}
