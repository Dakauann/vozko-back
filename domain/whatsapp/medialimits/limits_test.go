package medialimits

import (
	"testing"

	"vozko/domain/channel"
)

func TestTheCloudAPILimitsCoverEveryComposerKind(t *testing.T) {
	limits := Official()
	cases := []struct {
		kind     channel.MediaKind
		maxBytes int64
		allowed  string
		refused  string
	}{
		{channel.MediaImage, 5 << 20, "image/png", "image/webp"},
		{channel.MediaVideo, 16 << 20, "video/mp4", "video/webm"},
		{channel.MediaAudio, 16 << 20, "audio/webm", ""},
		{channel.MediaDocument, 100 << 20, "application/pdf", "application/zip"},
	}
	for _, tc := range cases {
		limit, ok := limits[tc.kind]
		if !ok || limit.MaxBytes != tc.maxBytes {
			t.Fatalf("%s = %+v, want %d bytes", tc.kind, limit, tc.maxBytes)
		}
		if !limit.Allows(tc.allowed) {
			t.Errorf("%s must allow %s", tc.kind, tc.allowed)
		}
		if tc.refused != "" && limit.Allows(tc.refused) {
			t.Errorf("%s must refuse %s", tc.kind, tc.refused)
		}
	}
	limits[channel.MediaImage] = channel.MediaLimit{}
	if Official()[channel.MediaImage].MaxBytes == 0 {
		t.Fatal("callers must get their own copy")
	}
}
