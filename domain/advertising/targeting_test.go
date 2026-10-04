package advertising

import (
	"slices"
	"testing"
)

func TestVideoOnlyPositionsAreRealPositions(t *testing.T) {
	video := VideoOnlyPositions()
	if !slices.Contains(video[PlatformFacebook], "instream_video") {
		t.Fatalf("Facebook in-stream reels only deliver video, got %v", video)
	}
	for platform, positions := range video {
		for _, position := range positions {
			if !slices.Contains(PlatformPositions()[platform], position) {
				t.Errorf("%s/%s is not a known position", platform, position)
			}
		}
	}
	for _, platform := range AutomaticPlatforms() {
		if _, ok := PlatformPositions()[platform]; !ok {
			t.Errorf("%s is not a known platform", platform)
		}
	}
}
