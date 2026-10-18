package unofficial_whatsapp_group_repository

import uwg "vozko/domain/unofficial_whatsapp_group"

func scopeSQL(alias string, s uwg.Scope) (string, []any) {
	if !s.Restrict {
		return "TRUE", nil
	}
	if len(s.InstanceIDs) == 0 {
		return "FALSE", nil
	}
	return `EXISTS (SELECT 1 FROM unofficial_whatsapp_groups seat
		WHERE seat.workspace_id = ` + alias + `.workspace_id AND seat.jid = ` + alias + `.jid
		  AND seat.deleted_at IS NULL AND seat.instance_id IN ?)`, []any{s.InstanceIDs}
}

const primaryConversationJoin = `JOIN LATERAL (
	SELECT c.id FROM unofficial_whatsapp_conversations c
	 WHERE c.instance_id = wg.primary_instance_id AND c.chat_id = wg.jid
	   AND c.campaign_id = '' AND c.deleted_at IS NULL
	 ORDER BY c.created_at DESC LIMIT 1) pc ON TRUE`

const primaryConversationLeftJoin = `LEFT JOIN LATERAL (
	SELECT c.id FROM unofficial_whatsapp_conversations c
	 WHERE c.instance_id = wg.primary_instance_id AND c.chat_id = wg.jid
	   AND c.campaign_id = '' AND c.deleted_at IS NULL
	 ORDER BY c.created_at DESC LIMIT 1) pc ON TRUE`
