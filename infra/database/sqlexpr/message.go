package sqlexpr

import (
	"fmt"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func SentByContact(alias string) string {
	return column(alias, "sender_kind") + " = " + Literal(string(conversation.SenderContact))
}

func SentAsReply(alias string) string {
	return column(alias, "sender_kind") + " IN (" + LiteralList(stringsOf(conversation.ReplySenderKinds())) + ")"
}

func SentOutbound(alias string) string {
	return column(alias, "sender_kind") + " IN (" + LiteralList(stringsOf(conversation.OutboundSenderKinds())) + ")"
}

func NotSeedPlaceholder(alias string) string {
	return "NOT (" + column(alias, "message_type") + " = " + Literal(string(conversation.MessageTypeSystem)) +
		" AND COALESCE(" + column(alias, "metadata") + "->>" + Literal(conversation.SeedMetadataKey) + ", '') = " +
		Literal(conversation.SeedSourceLeadImport) + ")"
}

func LiveMessage(alias string) string {
	return "COALESCE(" + column(alias, "metadata") + "->>" + Literal(conversation.BackfillMetadataKey) + ", '') <> 'true'"
}

func RealMessage(alias string) string {
	return NotSeedPlaceholder(alias) + " AND " + LiveMessage(alias)
}

func ServiceMessageIndexed(alias string) string {
	return fmt.Sprintf(`%s IS NULL
			  AND %s = %s
			  AND %s = %s
			  AND %s IN (%s)
			  AND COALESCE(%s, '') <> %s`,
		column(alias, "deleted_at"),
		column(alias, "entry_type"), Literal(string(shared.EntryTypeWhatsApp)),
		column(alias, "direction"), Literal(string(conversation.MessageDirectionOutbound)),
		column(alias, "message_type"), LiteralList(conversation.ServiceMessageTypeStrings()),
		column(alias, "sent_via"), Literal(string(conversation.MessageTransportBusinessApp)),
	)
}

func ServiceMessage(alias string) string {
	return fmt.Sprintf(`%s
			  AND %s IN (%s)`,
		ServiceMessageIndexed(alias),
		column(alias, "delivery_status"),
		LiteralList(conversation.BillableDeliveryStatusStrings()),
	)
}
