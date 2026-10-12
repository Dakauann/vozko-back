package studio

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func trackingFixture(t *testing.T) VideoDocument {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_tracking.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestTheEditorTrackingDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_tracking.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved a tracked document the backend refuses: %v", err)
	}
	doc := trackingFixture(t)
	shot, tag, rig := doc.Tracks[0].Clips[0], doc.Tracks[1].Clips[0], doc.Tracks[2].Clips[0]
	if len(shot.Trackers) != 2 || len(shot.Trackers[0].Samples) != 10 || shot.Trackers[0].Name != "Olho" || shot.Trackers[0].ReferenceMS != 133 {
		t.Fatalf("trackers lost: %+v", shot.Trackers)
	}
	s := shot.Stabilize
	if s == nil || s.TrackerID != "pt-eye" || s.SecondTrackerID != "pt-ear" || !s.Rotation || !s.Scale || s.SmoothMS != 600 || s.Zoom != 1.18 {
		t.Fatalf("stabilize lost: %+v", s)
	}
	if tag.ParentTracker != "pt-eye" || rig.ParentTracker != "pt-ear" || rig.Type != ClipNull {
		t.Fatalf("tracker links lost: %q %q", tag.ParentTracker, rig.ParentTracker)
	}
	again, _ := json.Marshal(doc)
	if err := ValidateDocument(KindVideo, again); err != nil {
		t.Fatalf("a round trip broke the document: %v", err)
	}
}

func TestTrackersAreStrict(t *testing.T) {
	cases := map[string]func(*VideoDocument){
		"a tracker on an overlay": func(d *VideoDocument) {
			d.Tracks[1].Clips[0].Trackers = d.Tracks[0].Clips[0].Trackers
		},
		"two trackers with one id": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Trackers[1].ID = "pt-eye"
		},
		"no samples": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Trackers[1].Samples = nil
		},
		"samples out of order": func(d *VideoDocument) {
			s := d.Tracks[0].Clips[0].Trackers[1].Samples
			s[1].AtMS, s[2].AtMS = s[2].AtMS, s[1].AtMS
		},
		"a reference that is not a frame": func(d *VideoDocument) { d.Tracks[0].Clips[0].Trackers[0].ReferenceMS = 140 },
		"a zero frame":                    func(d *VideoDocument) { d.Tracks[0].Clips[0].Trackers[0].FrameHeight = 0 },
		"a bad id":                        func(d *VideoDocument) { d.Tracks[0].Clips[0].Trackers[0].ID = "Pt Eye" },
		"a long name":                     func(d *VideoDocument) { d.Tracks[0].Clips[0].Trackers[0].Name = strings.Repeat("x", 200) },
		"a link to a missing tracker":     func(d *VideoDocument) { d.Tracks[1].Clips[0].ParentTracker = "ghost" },
		"a tracker link without a parent": func(d *VideoDocument) { d.Tracks[1].Clips[0].ParentID = "" },
		"stabilize on a missing tracker":  func(d *VideoDocument) { d.Tracks[0].Clips[0].Stabilize.TrackerID = "ghost" },
		"stabilize twice on one tracker":  func(d *VideoDocument) { d.Tracks[0].Clips[0].Stabilize.SecondTrackerID = "pt-eye" },
		"rotation without a second point": func(d *VideoDocument) { d.Tracks[0].Clips[0].Stabilize.SecondTrackerID = "" },
		"stabilize on a frozen clip":      func(d *VideoDocument) { d.Tracks[0].Clips[0].Freeze = true },
	}
	for name, change := range cases {
		d := trackingFixture(t)
		change(&d)
		if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; code != CodeInvalid {
			t.Errorf("%s: got %q", name, code)
		}
	}
	ranges := map[string]func(*VideoDocument){
		"a point off the frame":  func(d *VideoDocument) { d.Tracks[0].Clips[0].Trackers[0].Samples[3].X = 1.2 },
		"a confidence above one": func(d *VideoDocument) { d.Tracks[0].Clips[0].Trackers[0].Samples[3].Confidence = 1.5 },
		"a size too big":         func(d *VideoDocument) { d.Tracks[0].Clips[0].Trackers[0].Size = 0.8 },
		"a zoom below one":       func(d *VideoDocument) { d.Tracks[0].Clips[0].Stabilize.Zoom = 0.9 },
		"a zoom above four":      func(d *VideoDocument) { d.Tracks[0].Clips[0].Stabilize.Zoom = 4.5 },
		"a short smoothing":      func(d *VideoDocument) { d.Tracks[0].Clips[0].Stabilize.SmoothMS = 50 },
		"a long smoothing":       func(d *VideoDocument) { d.Tracks[0].Clips[0].Stabilize.SmoothMS = 20_000 },
	}
	for name, change := range ranges {
		d := trackingFixture(t)
		change(&d)
		if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; code != CodeOutOfRange {
			t.Errorf("%s: got %q", name, code)
		}
	}
}

func TestFractionalTrackMomentsAreRefused(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_tracking.json")
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(raw), `"atMs": 33,`, `"atMs": 33.5,`, 1)
	if broken == string(raw) || ValidateDocument(KindVideo, json.RawMessage(broken)) == nil {
		t.Fatal("a fractional sample moment was accepted")
	}
}
