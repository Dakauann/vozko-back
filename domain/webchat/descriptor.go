package webchat

import (
	"vozko/domain/channel"
	"vozko/domain/shared"
)

const (
	MaxAttachmentBytes = 10 << 20
	MaxOptionIDBytes   = 128
)

var AttachmentMIMETypes = map[string]channel.MediaKind{
	"image/jpeg":      channel.MediaImage,
	"image/png":       channel.MediaImage,
	"image/webp":      channel.MediaImage,
	"image/gif":       channel.MediaImage,
	"application/pdf": channel.MediaDocument,
}

func Descriptor() *channel.Descriptor {
	return &channel.Descriptor{
		Kind:      channel.KindWebchat,
		EntryType: shared.EntryTypeWebchat,
		Capabilities: channel.Capabilities{
			CanInitiateConversation: false,
			SupportsTemplates:       false,
			SupportsReactions:       false,
			SupportsTypingIndicator: true,
			SupportsReadReceipts:    false,
			SupportsRichText:        false,

			MaxTextRunes:   4096,
			OutboundWindow: 0,
			ExtendedWindow: 0,

			SignatureFormat: "%s:\n%s",

			MediaLimits: map[channel.MediaKind]channel.MediaLimit{
				channel.MediaImage: {
					MaxBytes:  MaxAttachmentBytes,
					MIMETypes: []string{"image/jpeg", "image/png", "image/webp", "image/gif"},
				},
				channel.MediaVideo:    {MaxBytes: 50 << 20},
				channel.MediaAudio:    {MaxBytes: 25 << 20},
				channel.MediaDocument: {MaxBytes: 50 << 20},
			},

			Interactive: channel.InteractiveLimits{
				MaxOptionsButtons:          MaxPendingOptions,
				MaxOptionsList:             MaxPendingOptions,
				MaxLabelRunes:              MaxOptionTitleRunes,
				MaxPayloadBytes:            MaxOptionIDBytes,
				SupportsOptionDescriptions: false,
			},
		},
		InboxSQL: channel.InboxSQL{
			EntryTable:         "webchat_conversations",
			ContactIDField:     "wcc.visitor_id::text",
			AccountIDField:     "COALESCE(wcc.widget_id::text, '')",
			ContainerIDField:   "wcw.id::text",
			ContainerNameField: "wcw.name",
			AutomationFields: "COALESCE(wcw.agent_id::text, '') AS agent_id, " +
				"COALESCE(wcw.workflow_id::text, '') AS workflow_id, " +
				"wcw.enable_agent_responses AS agent_responses_enabled, " +
				"wcw.enable_workflow AS workflow_enabled",
			EntryJoin: `JOIN webchat_conversations wcc ON wcc.id = %[1]s AND wcc.deleted_at IS NULL
			            JOIN webchat_widgets wcw ON wcw.id = wcc.widget_id`,
		},
	}
}
