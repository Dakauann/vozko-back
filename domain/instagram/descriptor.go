package instagram

import (
	"vozko/domain/channel"
	"vozko/domain/shared"
)

// Descriptor returns the channel registration for Instagram.
//
// This is the only place Instagram's channel-level behaviour and its inbox SQL
// live. When WhatsApp and the support widget are migrated onto the channel
// package they each add a sibling of this function, and the `switch entryType`
// statements they currently rely on get deleted.
func Descriptor() *channel.Descriptor {
	return &channel.Descriptor{
		Kind:      channel.KindInstagram,
		EntryType: shared.EntryTypeInstagram,
		Capabilities: channel.Capabilities{
			// A user must message the account first, there is no template or
			// opt-in list that lets us open a thread.
			CanInitiateConversation: false,
			SupportsTemplates:       false,
			SupportsReactions:       true,
			SupportsTypingIndicator: true,
			SupportsReadReceipts:    true,
			// Instagram DMs are plain text; WhatsApp's *bold* markup would be
			// shown literally.
			SupportsRichText: false,

			MaxTextBytes:   MaxTextBytes,
			OutboundWindow: MessagingWindow,
			ExtendedWindow: ExtendedMessagingWindow,

			SignatureFormat: "%s:\n%s",

			MediaLimits: map[channel.MediaKind]channel.MediaLimit{
				// Images cap at 8MB, the odd one out, and gif is unsupported.
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
				// pdf is the only supported document type.
				channel.MediaDocument: {
					MaxBytes:  25 * 1024 * 1024,
					MIMETypes: []string{"application/pdf"},
				},
			},

			// Quick replies. Instagram has ONE mechanism for a single choice, so
			// both prompt styles map to it and both caps are the same number.
			//
			// The generic template also carries buttons, but only 3 per element
			// and only postback/web_url, so it is strictly worse than quick
			// replies for this purpose and is not used here.
			Interactive: channel.InteractiveLimits{
				// "A maximum of 13 quick replies are supported."
				MaxOptionsButtons: MaxQuickReplies,
				MaxOptionsList:    MaxQuickReplies,
				// "Each quick reply allows up to 20 characters before being
				// truncated", truncated by Instagram, silently, which is
				// exactly the kind of thing the editor must warn about.
				MaxLabelRunes: MaxQuickReplyTitleRunes,
				// Instagram's own docs do not state a payload bound; this is the
				// Messenger Platform limit the Instagram surface inherits.
				MaxPayloadBytes: MaxQuickReplyPayloadBytes,
				// Quick replies are label-only, there is no description slot.
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
