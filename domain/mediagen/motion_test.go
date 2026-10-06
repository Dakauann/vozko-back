package mediagen

import "testing"

func TestAVisualClipMayMoveInAndOutFromAnEdge(t *testing.T) {
	req := validVideo()
	req.Video.Visual[0].Clips[0].MotionIn = &Motion{Edge: EdgeLeft, DurationMS: 500}
	req.Video.Visual[0].Clips[0].MotionOut = &Motion{Edge: EdgeBottom, DurationMS: 400}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAMotionKeepsItsRules(t *testing.T) {
	cases := map[string]struct {
		change func(*Timeline)
		code   string
	}{
		"unknown edge": {func(tl *Timeline) { tl.Visual[0].Clips[0].MotionIn = &Motion{Edge: "diagonal", DurationMS: 500} }, CodeUnknown},
		"too short":    {func(tl *Timeline) { tl.Visual[0].Clips[0].MotionIn = &Motion{Edge: EdgeTop, DurationMS: 50} }, CodeOutOfRange},
		"longer than clip": {func(tl *Timeline) {
			tl.Visual[0].Clips[0].MotionIn = &Motion{Edge: EdgeTop, DurationMS: 3_000}
			tl.Visual[0].Clips[0].MotionOut = &Motion{Edge: EdgeTop, DurationMS: 1_500}
		}, CodeOutOfRange},
		"on audio": {func(tl *Timeline) { tl.Audio[0].Clips[0].MotionOut = &Motion{Edge: EdgeRight, DurationMS: 500} }, CodeUnexpected},
	}
	for name, c := range cases {
		req := validVideo()
		c.change(&req.Video)
		if got := codesOf(t, req.Validate())[FieldTimeline]; got != c.code {
			t.Errorf("%s: code %q", name, got)
		}
	}
}

func TestAMotionChangesTheFingerprint(t *testing.T) {
	base := validVideo()
	moved := validVideo()
	moved.Video.Visual[0].Clips[0].MotionIn = &Motion{Edge: EdgeLeft, DurationMS: 500}
	other := validVideo()
	other.Video.Visual[0].Clips[0].MotionIn = &Motion{Edge: EdgeRight, DurationMS: 500}
	leaving := validVideo()
	leaving.Video.Visual[0].Clips[0].MotionOut = &Motion{Edge: EdgeLeft, DurationMS: 500}
	seen := map[string]bool{}
	for _, r := range []Request{base, moved, other, leaving} {
		fp := r.Fingerprint("u1")
		if seen[fp] {
			t.Fatalf("two different motions share a fingerprint")
		}
		seen[fp] = true
	}
}
