package rcs

import (
	"testing"

	"vozko/domain/channel"
)

func TestRCSCapabilities(t *testing.T) {
	caps := Capabilities()
	if !caps.CanInitiateConversation || caps.OutboundWindow != 0 || !caps.SupportsReadReceipts || caps.MaxTextRunes != MaxTextRunes {
		t.Fatalf("caps = %+v", caps)
	}
	if caps.Interactive.MaxOptionsButtons != MaxMessageSuggestions || caps.Interactive.MaxLabelRunes != MaxSuggestionLabelRunes || caps.Interactive.MaxPayloadBytes != MaxPostbackBytes {
		t.Fatalf("interactive = %+v", caps.Interactive)
	}
	for kind, mime := range map[channel.MediaKind]string{
		channel.MediaImage: "image/png", channel.MediaVideo: "video/mp4", channel.MediaAudio: "audio/mpeg", channel.MediaDocument: "application/pdf",
	} {
		limit, ok := caps.MediaLimits[kind]
		if !ok || !limit.Allows(mime) || limit.MaxBytes != MaxMediaBytes {
			t.Fatalf("%s limit = %+v", kind, limit)
		}
	}
	if caps.MediaLimits[channel.MediaDocument].Allows("application/zip") {
		t.Fatal("only PDF documents go over RCS")
	}
}
