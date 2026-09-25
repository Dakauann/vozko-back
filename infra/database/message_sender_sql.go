package database

import (
	"strings"

	"vozko/domain/conversation"
)

func SentByContactSQL(alias string) string {
	return senderKindColumn(alias) + " = '" + string(conversation.SenderContact) + "'"
}

func SentAsReplySQL(alias string) string {
	return senderKindColumn(alias) + " IN " + senderKindList(conversation.ReplySenderKinds())
}

func SentOutboundSQL(alias string) string {
	return senderKindColumn(alias) + " IN " + senderKindList(conversation.OutboundSenderKinds())
}

func senderKindColumn(alias string) string {
	if alias == "" {
		return "sender_kind"
	}
	return alias + ".sender_kind"
}

func senderKindList(kinds []conversation.SenderKind) string {
	quoted := make([]string, len(kinds))
	for i, kind := range kinds {
		quoted[i] = "'" + string(kind) + "'"
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}
