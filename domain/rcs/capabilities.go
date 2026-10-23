package rcs

import "vozko/domain/channel"

func Capabilities() channel.Capabilities {
	return channel.Capabilities{
		CanInitiateConversation: true,
		SupportsReadReceipts:    true,
		MaxTextRunes:            MaxTextRunes,
		SignatureFormat:         "%s:\n%s",
		MediaLimits: map[channel.MediaKind]channel.MediaLimit{
			channel.MediaImage:    {MaxBytes: MaxMediaBytes, MIMETypes: MediaMIMETypes(MediaImage)},
			channel.MediaVideo:    {MaxBytes: MaxMediaBytes, MIMETypes: MediaMIMETypes(MediaVideo)},
			channel.MediaAudio:    {MaxBytes: MaxMediaBytes, MIMETypes: MediaMIMETypes(MediaAudio)},
			channel.MediaDocument: {MaxBytes: MaxMediaBytes, MIMETypes: MediaMIMETypes(MediaPDF)},
		},
		Interactive: channel.InteractiveLimits{
			MaxOptionsButtons: MaxMessageSuggestions,
			MaxLabelRunes:     MaxSuggestionLabelRunes,
			MaxPayloadBytes:   MaxPostbackBytes,
		},
	}
}
