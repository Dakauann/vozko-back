package conversation

import (
	"encoding/json"
	"reflect"
	"testing"

	"vozko/domain/channel"
)

func TestMediaLimitsAreListedInKindOrderWithSortedTypes(t *testing.T) {
	limits := map[channel.MediaKind]channel.MediaLimit{
		channel.MediaDocument: {MaxBytes: 25 << 20, MIMETypes: []string{"text/plain", "application/pdf"}},
		channel.MediaImage:    {MaxBytes: 5 << 20, MIMETypes: []string{"image/png", "image/jpeg"}},
		channel.MediaAudio:    {MaxBytes: 16 << 20},
	}
	got := MediaLimitsOf(limits)
	want := []MediaLimit{
		{Kind: channel.MediaAudio, MaxBytes: 16 << 20, MIMETypes: []string{}},
		{Kind: channel.MediaDocument, MaxBytes: 25 << 20, MIMETypes: []string{"application/pdf", "text/plain"}},
		{Kind: channel.MediaImage, MaxBytes: 5 << 20, MIMETypes: []string{"image/jpeg", "image/png"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("limits =\n%+v\nwant\n%+v", got, want)
	}
	if limits[channel.MediaDocument].MIMETypes[0] != "text/plain" {
		t.Fatal("the channel's own list must not be reordered")
	}
	raw, _ := json.Marshal(got[0])
	if string(raw) != `{"kind":"audio","max_bytes":16777216,"mime_types":[]}` {
		t.Fatalf("wire = %s", raw)
	}
}

func TestAChannelWithoutMediaListsNoLimits(t *testing.T) {
	if got := MediaLimitsOf(nil); got == nil || len(got) != 0 {
		t.Fatalf("limits = %#v, want an empty list", got)
	}
}

func TestALimitWithoutSizeIsNotOffered(t *testing.T) {
	got := MediaLimitsOf(map[channel.MediaKind]channel.MediaLimit{channel.MediaVideo: {MIMETypes: []string{"video/mp4"}}})
	if len(got) != 0 {
		t.Fatalf("a limit without a size cannot be enforced by a client, got %+v", got)
	}
}
