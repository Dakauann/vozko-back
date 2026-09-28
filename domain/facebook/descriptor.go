package facebook

import (
	"time"

	"vozko/domain/channel"
	"vozko/domain/shared"
)

const (
	MaxTextRunes          = 2000
	MaxTemplateButtons    = 3
	MaxQuickReplies       = 13
	MaxOptionTitleRunes   = 20
	MaxOptionPayloadBytes = 1000
	MessagingWindow       = 24 * time.Hour
	HumanAgentWindow      = 7 * 24 * time.Hour
	maxImageBytes         = 8 << 20
	maxAttachmentBytes    = 25 << 20
)

func Descriptor(humanAgentApproved bool) *channel.Descriptor {
	return &channel.Descriptor{
		Kind:      channel.KindFacebook,
		EntryType: shared.EntryTypeFacebook,
		Capabilities: channel.Capabilities{
			CanInitiateConversation: false,
			SupportsTemplates:       false,
			SupportsReactions:       true,
			SupportsTypingIndicator: true,
			SupportsReadReceipts:    true,
			SupportsRichText:        false,

			MaxTextRunes:     MaxTextRunes,
			OutboundWindow:   MessagingWindow,
			ExtendedWindow:   HumanAgentWindow,
			HumanAgentWindow: humanAgentApproved,

			SignatureFormat: "%s:\n%s",

			MediaLimits: map[channel.MediaKind]channel.MediaLimit{
				channel.MediaImage: {MaxBytes: maxImageBytes, MIMETypes: []string{"image/jpeg", "image/png", "image/gif"}},
				channel.MediaVideo: {MaxBytes: maxAttachmentBytes, MIMETypes: []string{"video/mp4"}},
				channel.MediaAudio: {MaxBytes: maxAttachmentBytes, MIMETypes: []string{
					"audio/mpeg", "audio/mp4", "audio/m4a", "audio/aac", "audio/wav", "audio/x-wav",
				}},
				channel.MediaDocument: {MaxBytes: maxAttachmentBytes},
			},

			Interactive: channel.InteractiveLimits{
				MaxOptionsButtons:          MaxTemplateButtons,
				MaxOptionsList:             MaxQuickReplies,
				MaxLabelRunes:              MaxOptionTitleRunes,
				MaxPayloadBytes:            MaxOptionPayloadBytes,
				SupportsOptionDescriptions: false,
			},
		},
		InboxSQL: channel.InboxSQL{
			EntryTable:         "facebook_conversations",
			ContactIDField:     "fbc.contact_id::text",
			AccountIDField:     "COALESCE(fbc.page_id::text, '')",
			ContainerIDField:   "fbp.id::text",
			ContainerNameField: "fbp.name",
			AutomationFields: "COALESCE(fbp.agent_id::text, '') AS agent_id, " +
				"COALESCE(fbp.workflow_id::text, '') AS workflow_id, " +
				"fbp.enable_agent_responses AS agent_responses_enabled, " +
				"fbp.enable_workflow AS workflow_enabled",
			EntryJoin: `JOIN facebook_conversations fbc ON fbc.id = %[1]s AND fbc.deleted_at IS NULL
			            JOIN facebook_pages fbp ON fbp.id = fbc.page_id`,
		},
	}
}

func SendTierFor(tier channel.WindowTier) (SendTier, bool) {
	switch tier {
	case channel.WindowTierStandard:
		return SendStandard, true
	case channel.WindowTierHuman:
		return SendHumanAgent, true
	}
	return "", false
}

const (
	outboundMetadataPrefix = "vozko:"
	CaptionMetadata        = outboundMetadataPrefix + "caption"
)

func OutboundMetadata(humanInitiated bool) string {
	if humanInitiated {
		return outboundMetadataPrefix + "operator"
	}
	return outboundMetadataPrefix + "automation"
}
