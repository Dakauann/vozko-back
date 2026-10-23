package emailinbox

import (
	"vozko/domain/channel"
	"vozko/domain/emailsending"
	"vozko/domain/shared"
)

func Descriptor() *channel.Descriptor {
	return &channel.Descriptor{
		Kind:      channel.KindEmail,
		EntryType: shared.EntryTypeEmail,
		Capabilities: channel.Capabilities{
			CanInitiateConversation: false,
			SupportsTemplates:       false,
			SupportsReactions:       false,
			SupportsTypingIndicator: false,
			SupportsReadReceipts:    false,
			SupportsRichText:        false,

			MaxTextBytes:   MaxReplyTextBytes,
			OutboundWindow: 0,
			ExtendedWindow: 0,
			OutboundBudget: 0,

			SignatureFormat: "%s:\n%s",

			MediaLimits: map[channel.MediaKind]channel.MediaLimit{
				channel.MediaImage:    {MaxBytes: emailsending.MaxAttachmentBytes},
				channel.MediaVideo:    {MaxBytes: emailsending.MaxAttachmentBytes},
				channel.MediaAudio:    {MaxBytes: emailsending.MaxAttachmentBytes},
				channel.MediaDocument: {MaxBytes: emailsending.MaxAttachmentBytes},
			},
		},
		InboxSQL: channel.InboxSQL{
			EntryTable:         "email_conversations",
			ContactIDField:     "emc.contact_id::text",
			AccountIDField:     "COALESCE(emc.sender_id::text, '')",
			ContainerIDField:   "emi.id::text",
			ContainerNameField: "COALESCE(NULLIF(emi.name, ''), emi.address)",
			AutomationFields: "COALESCE(emi.agent_id::text, '') AS agent_id, " +
				"COALESCE(emi.workflow_id::text, '') AS workflow_id, " +
				"emi.enable_agent_responses AS agent_responses_enabled, " +
				"emi.enable_workflow AS workflow_enabled",
			EntryJoin: `JOIN email_conversations emc ON emc.id = %[1]s AND emc.deleted_at IS NULL
			            JOIN email_inboxes emi ON emi.id = emc.sender_id`,
		},
	}
}
