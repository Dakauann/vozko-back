package sqlexpr

import "vozko/domain/conversation"

const AwaitingReplySinceColumn = "awaiting_reply_since"

func WaitingSince(conversationAlias string) string {
	return "CASE WHEN " + column(conversationAlias, "conversation_status") +
		" = " + Literal(string(conversation.ConversationStatusFinished)) +
		" THEN NULL ELSE " + column(conversationAlias, AwaitingReplySinceColumn) + " END"
}

func FirstUnansweredCustomerMessage(entryID, entryType string) string {
	lastReply := "(SELECT MAX(lr.created_at) FROM conversation_messages lr" +
		" WHERE lr.entry_id = " + entryID + " AND lr.entry_type = " + entryType +
		" AND lr.deleted_at IS NULL AND " + SentAsReply("lr") + ")"
	return "(SELECT MIN(fu.created_at) FROM conversation_messages fu" +
		" WHERE fu.entry_id = " + entryID + " AND fu.entry_type = " + entryType +
		" AND fu.deleted_at IS NULL AND " + SentByContact("fu") +
		" AND fu.created_at > COALESCE(" + lastReply + ", '-infinity'::timestamptz))"
}
