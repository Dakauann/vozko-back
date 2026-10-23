package sms

import (
	"vozko/domain/channel"
	"vozko/domain/shared"
)

func Descriptor() *channel.Descriptor {
	return &channel.Descriptor{
		Kind:         channel.KindSMS,
		EntryType:    shared.EntryTypeSMS,
		Capabilities: Capabilities(),
		InboxSQL: channel.InboxSQL{
			EntryTable:         "sms_conversations",
			ContactIDField:     "smc.contact_id::text",
			AccountIDField:     "COALESCE(smc.sender_id::text, '')",
			ContainerIDField:   "smr.id::text",
			ContainerNameField: "COALESCE(NULLIF(smr.brand_prefix, ''), smr.id::text)",
			AutomationFields: "COALESCE(smr.agent_id::text, '') AS agent_id, " +
				"COALESCE(smr.workflow_id::text, '') AS workflow_id, " +
				"smr.enable_agent_responses AS agent_responses_enabled, " +
				"smr.enable_workflow AS workflow_enabled",
			EntryJoin: `JOIN sms_conversations smc ON smc.id = %[1]s AND smc.deleted_at IS NULL
			            JOIN sms_routes smr ON smr.id = smc.sender_id`,
		},
	}
}
