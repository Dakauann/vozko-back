package rcs

import (
	"vozko/domain/channel"
	"vozko/domain/shared"
)

func Descriptor() *channel.Descriptor {
	return &channel.Descriptor{
		Kind:         channel.KindRCS,
		EntryType:    shared.EntryTypeRCS,
		Capabilities: Capabilities(),
		InboxSQL: channel.InboxSQL{
			EntryTable:         "rcs_conversations",
			ContactIDField:     "rcc.contact_id::text",
			AccountIDField:     "COALESCE(rcc.sender_id::text, '')",
			ContainerIDField:   "rcsd.id::text",
			ContainerNameField: "COALESCE(NULLIF(rcsd.brand_name, ''), rcsd.id::text)",
			AutomationFields: "COALESCE(rcsd.agent_id::text, '') AS agent_id, " +
				"COALESCE(rcsd.workflow_id::text, '') AS workflow_id, " +
				"rcsd.enable_agent_responses AS agent_responses_enabled, " +
				"rcsd.enable_workflow AS workflow_enabled",
			EntryJoin: `JOIN rcs_conversations rcc ON rcc.id = %[1]s AND rcc.deleted_at IS NULL
			            JOIN rcs_senders rcsd ON rcsd.id = rcc.sender_id`,
		},
	}
}
