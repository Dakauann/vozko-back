package unofficial_whatsapp

import (
	"vozko/domain/channel"
	"vozko/domain/shared"
)

func Descriptor() *channel.Descriptor {
	return &channel.Descriptor{
		Kind:      channel.KindUnofficialWhatsApp,
		EntryType: shared.EntryTypeUnofficialWhatsApp,
		Capabilities: channel.Capabilities{
			CanInitiateConversation: true,
			SupportsTemplates:       false,
			SupportsReactions:       true,
			SupportsTypingIndicator: true,
			SupportsReadReceipts:    true,
			SupportsRichText:        true,

			MaxTextRunes:   MaxTextRunes,
			OutboundWindow: 0,
			ExtendedWindow: 0,

			SignatureFormat: "*%s*:\n%s",

			MediaLimits: map[channel.MediaKind]channel.MediaLimit{
				channel.MediaImage: {
					MaxBytes:  MaxImageBytes,
					MIMETypes: []string{"image/jpeg", "image/png", "image/webp"},
				},
				channel.MediaVideo: {
					MaxBytes:  MaxVideoBytes,
					MIMETypes: []string{"video/mp4", "video/3gpp"},
				},
				channel.MediaAudio: {
					MaxBytes: MaxAudioBytes,
					MIMETypes: []string{
						"audio/aac", "audio/mp4", "audio/mpeg", "audio/amr",
						"audio/ogg", "audio/opus", "audio/wav", "audio/x-wav",
						"audio/webm", "audio/m4a", "audio/x-m4a", "audio/3gpp",
					},
				},
				channel.MediaDocument: {MaxBytes: MaxDocumentBytes},
			},

			Interactive: channel.InteractiveLimits{
				MaxOptionsButtons:          MaxButtonOptions,
				MaxOptionsList:             MaxListOptions,
				MaxLabelRunes:              MaxOptionLabelRunes,
				MaxPayloadBytes:            MaxOptionPayloadBytes,
				SupportsOptionDescriptions: true,
			},
		},
		InboxSQL: channel.InboxSQL{
			EntryTable:         "unofficial_whatsapp_conversations",
			ContactIDField:     "uwc.contact_id::text",
			AccountIDField:     "COALESCE(uwc.instance_id::text, '')",
			ContainerIDField:   "uwi.id::text",
			ContainerNameField: "uwi.display_name",
			AutomationFields: "COALESCE(uwi.agent_id::text, '') AS agent_id, " +
				"COALESCE(uwi.workflow_id::text, '') AS workflow_id, " +
				"uwi.enable_agent_responses AS agent_responses_enabled, " +
				"uwi.enable_workflow AS workflow_enabled",
			EntryJoin: `JOIN unofficial_whatsapp_conversations uwc ON uwc.id = %[1]s AND uwc.deleted_at IS NULL
			            JOIN unofficial_whatsapp_instances uwi ON uwi.id = uwc.instance_id`,
		},
	}
}
