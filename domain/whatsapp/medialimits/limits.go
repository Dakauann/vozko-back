package medialimits

import "vozko/domain/channel"

func Official() map[channel.MediaKind]channel.MediaLimit {
	return map[channel.MediaKind]channel.MediaLimit{
		channel.MediaImage: {MaxBytes: 5 << 20, MIMETypes: []string{"image/jpeg", "image/png"}},
		channel.MediaVideo: {MaxBytes: 16 << 20, MIMETypes: []string{"video/3gpp", "video/mp4"}},
		channel.MediaAudio: {MaxBytes: 16 << 20},
		channel.MediaDocument: {MaxBytes: 100 << 20, MIMETypes: []string{
			"text/plain",
			"application/pdf",
			"application/msword",
			"application/vnd.ms-excel",
			"application/vnd.ms-powerpoint",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		}},
	}
}
