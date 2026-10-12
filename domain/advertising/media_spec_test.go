package advertising

import "testing"

func squareImage(side int) MediaFacts {
	return MediaFacts{Kind: MediaImage, Container: ContainerPNG, Bytes: 2 << 20, Width: side, Height: side}
}

func verticalVideo() MediaFacts {
	return MediaFacts{Kind: MediaVideo, Container: ContainerMP4, Bytes: 40 << 20, Width: 1080, Height: 1920, DurationMS: 15_000}
}

func TestTheCommonAdShapesMeetTheMediaSpec(t *testing.T) {
	cases := map[string]MediaFacts{
		"square image":    squareImage(1080),
		"feed portrait":   {Kind: MediaImage, Container: ContainerJPEG, Bytes: 1 << 20, Width: 1080, Height: 1350},
		"story image":     {Kind: MediaImage, Container: ContainerPNG, Bytes: 3 << 20, Width: 1080, Height: 1920},
		"link landscape":  {Kind: MediaImage, Container: ContainerJPEG, Bytes: 1 << 20, Width: 1200, Height: 628},
		"vertical video":  verticalVideo(),
		"widescreen":      {Kind: MediaVideo, Container: ContainerMOV, Bytes: 90 << 20, Width: 1920, Height: 1080, DurationMS: 60_000},
		"square video 1s": {Kind: MediaVideo, Container: ContainerMP4, Bytes: 1 << 20, Width: 600, Height: 600, DurationMS: MinVideoMS},
	}
	for name, facts := range cases {
		if code := facts.Issue(); code != "" {
			t.Errorf("%s must be accepted, got %q", name, code)
		}
	}
}

func TestMediaOutsideTheSpecIsRefusedWithItsReason(t *testing.T) {
	cases := []struct {
		name  string
		facts MediaFacts
		want  string
	}{
		{"webp image", MediaFacts{Kind: MediaImage, Container: "webp", Bytes: 1 << 20, Width: 1080, Height: 1080}, CodeMediaFormat},
		{"webm video", MediaFacts{Kind: MediaVideo, Container: "webm", Bytes: 1 << 20, Width: 1080, Height: 1920, DurationMS: 10_000}, CodeMediaFormat},
		{"heavy image", MediaFacts{Kind: MediaImage, Container: ContainerPNG, Bytes: MaxImageBytes + 1, Width: 4000, Height: 4000}, CodeMediaTooLarge},
		{"tiny image", squareImage(MinImageSide - 1), CodeMediaTooSmall},
		{"narrow video", MediaFacts{Kind: MediaVideo, Container: ContainerMP4, Bytes: 1 << 20, Width: MinVideoSide - 1, Height: 200, DurationMS: 5_000}, CodeMediaTooSmall},
		{"banner image", MediaFacts{Kind: MediaImage, Container: ContainerPNG, Bytes: 1 << 20, Width: 3000, Height: 1000}, CodeMediaRatio},
		{"tower image", MediaFacts{Kind: MediaImage, Container: ContainerPNG, Bytes: 1 << 20, Width: 1000, Height: 2500}, CodeMediaRatio},
		{"cinema video", MediaFacts{Kind: MediaVideo, Container: ContainerMP4, Bytes: 1 << 20, Width: 2400, Height: 1000, DurationMS: 5_000}, CodeMediaRatio},
		{"blink video", MediaFacts{Kind: MediaVideo, Container: ContainerMP4, Bytes: 1 << 20, Width: 1080, Height: 1080, DurationMS: MinVideoMS - 1}, CodeVideoTooShort},
		{"endless video", MediaFacts{Kind: MediaVideo, Container: ContainerMP4, Bytes: 1 << 20, Width: 1080, Height: 1080, DurationMS: MaxVideoMS + 1}, CodeVideoTooLong},
		{"unread size", MediaFacts{Kind: MediaImage, Container: ContainerPNG, Bytes: 1 << 20}, CodeMediaUnreadable},
		{"unknown kind", MediaFacts{Kind: "audio", Container: ContainerMP4, Bytes: 1 << 20, Width: 1080, Height: 1080}, CodeMediaFormat},
	}
	for _, c := range cases {
		if got := c.facts.Issue(); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTheRatioToleranceAcceptsANearMiss(t *testing.T) {
	nearStory := MediaFacts{Kind: MediaImage, Container: ContainerPNG, Bytes: 1 << 20, Width: 1060, Height: 1920}
	if code := nearStory.Issue(); code != "" {
		t.Fatalf("a story a few pixels off 9:16 must pass, got %q", code)
	}
}

func TestEveryMediaCodeIsListed(t *testing.T) {
	codes := MediaSpecCodes()
	for _, code := range []string{CodeMediaFormat, CodeMediaTooLarge, CodeMediaTooSmall, CodeMediaRatio, CodeVideoTooShort, CodeVideoTooLong, CodeMediaUnreadable} {
		found := false
		for _, listed := range codes {
			found = found || listed == code
		}
		if !found {
			t.Errorf("code %q is not listed", code)
		}
	}
}
