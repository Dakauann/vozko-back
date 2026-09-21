package instagram

import (
	"vozko/domain/channel"
	"vozko/domain/shared"
)

func Descriptor() *channel.Descriptor {
	return &channel.Descriptor{
		Kind:      channel.KindInstagram,
		EntryType: shared.EntryTypeInstagram,
		Capabilities: channel.Capabilities{
			CanInitiateConversation: false,
			SupportsTemplates:       false,
			SupportsReactions:       true,
			SupportsTypingIndicator: true,
			SupportsReadReceipts:    true,
			SupportsRichText:        false,

			MaxTextBytes:   MaxTextBytes,
			OutboundWindow: MessagingWindow,
			ExtendedWindow: ExtendedMessagingWindow,

			SignatureFormat: "%s:\n%s",

			MediaLimits: map[channel.MediaKind]channel.MediaLimit{
				channel.MediaImage: {
					MaxBytes:  8 * 1024 * 1024,
					MIMETypes: []string{"image/png", "image/jpeg"},
				},
				channel.MediaVideo: {
					MaxBytes: 25 * 1024 * 1024,
					MIMETypes: []string{
						"video/mp4", "video/ogg", "video/x-msvideo",
						"video/quicktime", "video/webm",
					},
				},
				channel.MediaAudio: {
					MaxBytes: 25 * 1024 * 1024,
					MIMETypes: []string{
						"audio/aac", "audio/mp4", "audio/m4a",
						"audio/wav", "audio/x-wav", "audio/mpeg",
					},
				},
				channel.MediaDocument: {
					MaxBytes:  25 * 1024 * 1024,
					MIMETypes: []string{"application/pdf"},
				},
			},

			Interactive: channel.InteractiveLimits{
				MaxOptionsButtons:          MaxQuickReplies,
				MaxOptionsList:             MaxQuickReplies,
				MaxLabelRunes:              MaxQuickReplyTitleRunes,
				MaxPayloadBytes:            MaxQuickReplyPayloadBytes,
				SupportsOptionDescriptions: false,
			},
		},
		InboxSQL: channel.InboxSQL{
			EntryTable:         "instagram_conversations",
			ContactIDField:     "igc.contact_id::text",
			AccountIDField:     "COALESCE(igc.ig_account_id::text, '')",
			ContainerIDField:   "iga.id::text",
			ContainerNameField: "iga.username",
			AutomationFields: "COALESCE(iga.agent_id::text, '') AS agent_id, " +
				"COALESCE(iga.workflow_id::text, '') AS workflow_id, " +
				"iga.enable_agent_responses AS agent_responses_enabled, " +
				"iga.enable_workflow AS workflow_enabled",
			EntryJoin: `JOIN instagram_conversations igc ON igc.id = %[1]s AND igc.deleted_at IS NULL
			            JOIN instagram_accounts iga ON iga.id = igc.ig_account_id`,
		},
	}
}
