package media

import "testing"

func TestOnlyAudioOfTheSameWorkspaceIsPlayableOnACall(t *testing.T) {
	audio := &Media{WorkspaceID: "ws1", Type: MediaTypeAudio, URL: "https://files/a.mp3"}
	cases := []struct {
		name      string
		media     *Media
		workspace string
		want      bool
	}{
		{"audio of the workspace", audio, "ws1", true},
		{"audio of another workspace", audio, "ws2", false},
		{"no workspace given", audio, " ", false},
		{"an image", &Media{WorkspaceID: "ws1", Type: MediaTypeProductImage, URL: "https://files/a.png"}, "ws1", false},
		{"audio without a file", &Media{WorkspaceID: "ws1", Type: MediaTypeAudio}, "ws1", false},
		{"missing media", nil, "ws1", false},
	}
	for _, tc := range cases {
		if got := tc.media.PlayableOnCallFor(tc.workspace); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
