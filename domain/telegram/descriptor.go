package telegram

import (
	"vozko/domain/channel"
	"vozko/domain/shared"
)

// Descriptor returns the channel registration for Telegram.
//
// This is the only place Telegram's channel-level behaviour lives. Note the two
// values that differ from every other channel we carry:
//
//   - MaxTextRunes, not MaxTextBytes. Telegram counts characters; Instagram
//     counts bytes. Capabilities.TextTooLong applies whichever is set so no
//     caller has to remember which.
//   - OutboundWindow is ZERO. Bot mode has no messaging window at all, and
//     business mode's 24h window is a per-ACCOUNT fact resolved in
//     ChannelAdapter.WindowState. Encoding a window here would disable the
//     composer for every bot-mode conversation older than a day.
func Descriptor() *channel.Descriptor {
	return &channel.Descriptor{
		Kind:      channel.KindTelegram,
		EntryType: shared.EntryTypeTelegram,
		Capabilities: channel.Capabilities{
			// A bot cannot message a user who never started it. Our conversations
			// only exist because the customer wrote first, so this is satisfied by
			// construction — but the CRM must not offer an "open a conversation"
			// affordance, because there is none. Deep links are the substitute.
			CanInitiateConversation: false,
			SupportsTemplates:       false,
			// Outbound reactions work (one per message). Inbound reactions require
			// the bot to be a chat administrator, which does not exist in a private
			// chat, so nothing is promised on the receive side.
			SupportsReactions:       true,
			SupportsTypingIndicator: true,
			// Read receipts exist only in business mode, only outbound (we mark the
			// customer's message read on the owner's behalf). We never learn whether
			// the customer read ours — Telegram has no delivery or read callback at
			// all — so the CRM must not render a status track that can never fill.
			SupportsReadReceipts: false,
			// Telegram renders HTML and MarkdownV2. HTML is the one we use:
			// MarkdownV2 requires escaping a long list of characters and a stray
			// underscore in a customer's name fails the whole send.
			SupportsRichText: true,

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
				// sendDocument accepts anything, unlike Instagram's PDF-only file
				// attachment. A nil MIME list means exactly that.
				channel.MediaDocument: {MaxBytes: MaxUploadOtherBytes},
			},

			// Inline keyboards. Telegram is the most permissive channel we carry
			// on count and the strictest on payload.
			Interactive: channel.InteractiveLimits{
				// The Bot API documents NO limit on inline_keyboard length —
				// it is "Array of Array of InlineKeyboardButton" and nothing
				// more. MaxInlineKeyboardButtons is therefore OUR cap, not
				// Telegram's, chosen so a runaway workflow config cannot build a
				// message no human can use. Both styles get the same number
				// because Telegram renders one mechanism either way.
				MaxOptionsButtons: MaxInlineKeyboardButtons,
				MaxOptionsList:    MaxInlineKeyboardButtons,
				// No documented label limit.
				MaxLabelRunes: 0,
				// "callback_data | String | Optional. Data to be sent in a
				// callback query to the bot when the button is pressed, 1-64
				// bytes". This is the tightest payload bound in the system.
				MaxPayloadBytes: MaxCallbackDataBytes,
				// Inline buttons are label-only — there is no second line under a button.
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
