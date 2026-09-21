package telegram

import (
	"vozko/domain/channel"
	"vozko/domain/shared"
)

func Descriptor() *channel.Descriptor {
	return &channel.Descriptor{
		Kind:      channel.KindTelegram,
		EntryType: shared.EntryTypeTelegram,
		Capabilities: channel.Capabilities{
			CanInitiateConversation: false,
			SupportsTemplates:       false,
			SupportsReactions:       true,
			SupportsTypingIndicator: true,
			SupportsReadReceipts:    false,
			SupportsRichText:        true,

			MaxTextRunes:   MaxTextRunes,
			OutboundWindow: 0,
			ExtendedWindow: 0,

			SignatureFormat: "<b>%s</b>:\n%s",

			MediaLimits: map[channel.MediaKind]channel.MediaLimit{
				channel.MediaImage: {
					MaxBytes:  MaxUploadPhotoBytes,
					MIMETypes: []string{"image/jpeg", "image/png", "image/webp", "image/gif"},
				},
				channel.MediaVideo: {
					MaxBytes:  MaxUploadOtherBytes,
					MIMETypes: []string{"video/mp4", "video/quicktime", "video/webm", "video/x-msvideo"},
				},
				channel.MediaAudio: {
					MaxBytes: MaxUploadOtherBytes,
					MIMETypes: []string{
						"audio/mpeg", "audio/mp4", "audio/ogg", "audio/aac",
						"audio/wav", "audio/x-wav", "audio/m4a",
					},
				},
				channel.MediaDocument: {MaxBytes: MaxUploadOtherBytes},
			},

			Interactive: channel.InteractiveLimits{
				MaxOptionsButtons:          MaxInlineKeyboardButtons,
				MaxOptionsList:             MaxInlineKeyboardButtons,
				MaxLabelRunes:              0,
				MaxPayloadBytes:            MaxCallbackDataBytes,
				SupportsOptionDescriptions: false,
			},
		},
		InboxSQL: channel.InboxSQL{
			EntryTable:         "telegram_conversations",
			ContactIDField:     "tgc.contact_id::text",
			AccountIDField:     "COALESCE(tgc.account_id::text, '')",
			ContainerIDField:   "tga.id::text",
			ContainerNameField: "tga.bot_username",
			AutomationFields: "COALESCE(tga.agent_id::text, '') AS agent_id, " +
				"COALESCE(tga.workflow_id::text, '') AS workflow_id, " +
				"tga.enable_agent_responses AS agent_responses_enabled, " +
				"tga.enable_workflow AS workflow_enabled",
			EntryJoin: `JOIN telegram_conversations tgc ON tgc.id = %[1]s AND tgc.deleted_at IS NULL
			            JOIN telegram_accounts tga ON tga.id = tgc.account_id`,
		},
	}
}
