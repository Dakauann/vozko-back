package database

import (
	"fmt"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// The rule that decides what Meta charges us for, rendered as SQL exactly once.
//
// Two things have to agree about it: the reporting query, and the partial index
// that makes the reporting query fast. Postgres only uses a partial index when
// it can prove the query's WHERE implies the index's predicate, so the moment
// these two are written out separately and one of them drifts, the index stops
// matching and the report silently goes back to scanning three million rows.
// Building both from here removes the opportunity.
//
// Everything is inlined rather than bound for the same reason: a bind parameter
// cannot be proven to imply a constant predicate under a generic plan. The
// values are compile-time constants from domain/conversation, never request
// data.

// qualify prefixes a column with a table alias, or returns it bare.
//
// The index predicate is written against the table itself and cannot use an
// alias; the query joins four tables and must. Same predicate, two renderings.
func qualify(alias, column string) string {
	if alias == "" {
		return column
	}
	return alias + "." + column
}

// ServiceMessageIndexPredicateSQL is the stable part of the rule: what the
// message IS, independent of how its delivery went.
//
// delivery_status is deliberately absent. It mutates on every status webhook
// (sent, then delivered, then read), so keying the index on it would move rows
// in and out of the index on the busiest write path the platform has. The
// query applies it on top, which still lets the planner use the index because a
// narrower query implies a wider index predicate.
func ServiceMessageIndexPredicateSQL(alias string) string {
	return fmt.Sprintf(`%s IS NULL
			  AND %s = %s
			  AND %s = %s
			  AND %s IN (%s)
			  AND COALESCE(%s, '') <> %s`,
		qualify(alias, "deleted_at"),
		qualify(alias, "entry_type"), SQLStringLiteral(string(shared.EntryTypeWhatsApp)),
		qualify(alias, "direction"), SQLStringLiteral(string(conversation.MessageDirectionOutbound)),
		qualify(alias, "message_type"), SQLStringLiteralList(conversation.ServiceMessageTypeStrings()),
		// Coexistence puts the WhatsApp Business app and the Cloud API on one
		// number. When the owner replies from the app, Meta echoes that message
		// to us and we store it exactly like our own: same channel, same entry
		// type, same operator type. Meta does not bill it, because only what
		// goes out through the API is billed, so counting it would invent a cost.
		//
		// COALESCE rather than a bare comparison because NULL <> 'x' is NULL,
		// which would quietly drop every row written before the column existed.
		// The query renders the identical expression, so the index still matches.
		qualify(alias, "sent_via"), SQLStringLiteral(string(conversation.MessageTransportBusinessApp)),
	)
}

// ServiceMessagePredicateSQL is the whole rule: what the message is, plus the
// requirement that Meta actually accepted it.
//
// Meta charges on delivery, so a send the provider rejected costs nothing.
func ServiceMessagePredicateSQL(alias string) string {
	return fmt.Sprintf(`%s
			  AND %s IN (%s)`,
		ServiceMessageIndexPredicateSQL(alias),
		qualify(alias, "delivery_status"),
		SQLStringLiteralList(conversation.BillableDeliveryStatusStrings()),
	)
}
