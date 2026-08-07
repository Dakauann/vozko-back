package unofficial_whatsapp

import (
	"vozko/domain/channel"
	"vozko/domain/shared"
)

// Descriptor returns the channel registration.
//
// Read against the Cloud API's descriptor, the differences are the whole reason
// this is a separate entry type:
//
//   - CanInitiateConversation is TRUE and SupportsTemplates is FALSE. Cold
//     outbound works without a template. That is what customers want from this
//     channel and simultaneously the reason it can get their number banned.
//   - OutboundWindow is ZERO. There is no 24h rule. The composer closes on
//     session loss, on a WhatsApp restriction, or on a block — all facts about
//     the instance or contact, none of them a clock, which is why they are
//     resolved in the send adapter's WindowState rather than encoded here.
//   - SupportsReadReceipts is TRUE. Unlike Telegram, this channel delivers real
//     Sent/Delivered/Read callbacks, so the CRM's status track is honest and
//     must be rendered.
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
			// WhatsApp renders *bold* / _italic_ / ~strike~ / ```mono```, not
			// HTML. Reusing Telegram's HTML signature here would print literal
			// tags into a customer's chat.
			SupportsRichText: true,

			// Runes, not bytes: the provider documents no text limit, so this is
			// our own cap and it is measured the way WhatsApp's own client
			// counts.
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
				// What an operator may HAND us, not what WhatsApp accepts raw:
				// the send path re-encodes every audio to ogg/opus, so the list
				// covers what the CRM recorder and a file picker produce. It
				// previously mirrored WhatsApp's published list, which rejected
				// audio/wav — the one format the recorder always emits.
				channel.MediaAudio: {
					MaxBytes: MaxAudioBytes,
					MIMETypes: []string{
						"audio/aac", "audio/mp4", "audio/mpeg", "audio/amr",
						"audio/ogg", "audio/opus", "audio/wav", "audio/x-wav",
						"audio/webm", "audio/m4a", "audio/x-m4a", "audio/3gpp",
					},
				},
				// A nil MIME list means any type, which is correct here: the
				// document send path accepts anything WhatsApp accepts.
				channel.MediaDocument: {MaxBytes: MaxDocumentBytes},
			},

			// WhatsApp's own split, not ours: three buttons is a different
			// message type from a ten-row list, and list rows are the only
			// option slot in the system that carries a description line.
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
