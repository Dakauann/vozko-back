package database

import (
	"fmt"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func qualify(alias, column string) string {
	if alias == "" {
		return column
	}
	return alias + "." + column
}

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
		qualify(alias, "sent_via"), SQLStringLiteral(string(conversation.MessageTransportBusinessApp)),
	)
}

func ServiceMessagePredicateSQL(alias string) string {
	return fmt.Sprintf(`%s
			  AND %s IN (%s)`,
		ServiceMessageIndexPredicateSQL(alias),
		qualify(alias, "delivery_status"),
		SQLStringLiteralList(conversation.BillableDeliveryStatusStrings()),
	)
}
