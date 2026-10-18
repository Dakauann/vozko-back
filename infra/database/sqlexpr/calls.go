package sqlexpr

import "vozko/domain/calls/cdr"

const NoCallback = "'infinity'::timestamptz"

func CallCounterpart(alias string) string {
	return "(CASE WHEN " + column(alias, "direction") + " = " + Literal(string(cdr.DirectionInbound)) +
		" THEN " + column(alias, "phone_from") + " ELSE " + column(alias, "phone_to") + " END)"
}

func CallListAgenda(alias string) string {
	return "COALESCE(" + column(alias, "callback_at") + ", " + NoCallback + ")"
}
